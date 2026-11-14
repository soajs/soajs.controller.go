package middleware

import (
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/urac"
)

// MT is the multi-tenant middleware before OAuth, as in Node.js (mw/mt/index.js and the first
// checks of mw/mt/utils.js). For a service version with extKeyRequired:
//   - a key without a package answers 153, a package that did not load 152;
//   - the tenant and its oauth come from the key;
//   - the custom registry mt.whitelist can skip the ACL and OAuth checks;
//   - the extKey's geo allow and deny lists answer 155, its device allow and deny lists 156;
//   - for a version with oauth, OAuth is required unless the API is public (oauthCheck).
//
// Without extKeyRequired the API is public and OAuth runs when the version has oauth.
func MT(prov provision.Provision, reg registry.Registry, uracDriver *urac.Driver, env string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}
			params := soajsCtx.Controller.ServiceParams
			serviceName := params.URLInfo.ServiceName
			version := versionFlags(reg, serviceName, params.URLInfo.ServiceVersion)
			params.Environment = env
			params.ServiceName = serviceName
			params.ServiceVersion = params.URLInfo.ServiceVersion
			params.UracDriver = uracDriver

			if serviceName == "soajs" || !version.ExtKeyRequired {
				params.IsAPIPublic = true
				params.RequiresOAuth = serviceName != "soajs" && version.OAuth
				next.ServeHTTP(w, r)
				return
			}

			key := soajsCtx.Key
			if key == nil || key.Application.Package == "" {
				WriteCode(w, 153)
				return
			}
			if params.Package == nil {
				WriteCode(w, 152)
				return
			}
			soajsCtx.Tenant = types.TenantFromKey(key)
			params.KeyAPI = true
			tenantOauth, _ := prov.GetTenantOauth(key.Tenant.ID)
			params.TenantOauth = tenantOauth

			params.SkipACL, params.SkipOAuth = CheckMTWhitelist(reg.Get(), getClientIP(r))

			if !params.SkipACL && !geoAllowed(key.Geo, getClientIP(r)) {
				WriteCode(w, 155)
				return
			}
			if !params.SkipACL {
				if code := deviceCheck(key.Device, r.Header.Get("User-Agent")); code != 0 {
					WriteCode(w, code)
					return
				}
			}

			if version.OAuth && !params.SkipOAuth && reg.Get() != nil && reg.Get().ServiceConfig != nil {
				disabled := tenantOauth != nil && tenantOauth.Disabled != 0
				if !disabled && !isAPIPublic(params.ACL, params.URLInfo.ServicePath, r) {
					params.RequiresOAuth = true
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// MTACL runs the ACL checks after OAuth and the user (serviceCheck and apiCheck of the Node.js
// mt): a service the package does not grant answers 154; then the API: 158 when the service
// needs a logged-in user and there is none, 159 when apisPermission is restricted and the API is
// not listed, 161 when the API needs a logged-in user and there is none.
func MTACL(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}
			params := soajsCtx.Controller.ServiceParams
			if params.URLInfo.ServiceName == "soajs" || params.SkipACL ||
				!versionFlags(reg, params.URLInfo.ServiceName, params.URLInfo.ServiceVersion).ExtKeyRequired {
				next.ServeHTTP(w, r)
				return
			}
			if params.ACL == nil {
				WriteCode(w, 154)
				return
			}
			if code := apiCheck(params, params.UserInfo != nil); code != 0 {
				WriteCode(w, code)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// apiCheck is the Node.js apiCheck for the latest ACL shape, where access is a boolean. It
// returns the error code, 0 when the API may be called, and sets IsAPIPublic.
func apiCheck(params *types.ServiceParams, loggedIn bool) int {
	acl := params.ACL
	api := acl.API(params.URLInfo.ServicePath)
	if acl.Access {
		if api != nil && !api.Access {
			params.IsAPIPublic = true
		}
		if !loggedIn && (api == nil || api.Access) {
			return 158
		}
		return checkPermission(acl, api, loggedIn)
	}
	if api == nil || !api.Access {
		params.IsAPIPublic = true
	}
	if api != nil || acl.APIsPermission == "restricted" {
		return checkPermission(acl, api, loggedIn)
	}
	return 0
}

// checkPermission is the Node.js _api.checkPermission.
func checkPermission(acl *types.FinalACL, api *types.ACLAPI, loggedIn bool) int {
	if acl.APIsPermission == "restricted" && api == nil {
		return 159
	}
	if api == nil || !api.Access {
		return 0
	}
	if !loggedIn {
		return 161
	}
	return 0
}

// isAPIPublic is the public decision of the Node.js oauthCheck: the API needs no token when the
// service version is open and the API is open or unlisted, or the API itself is open; a token in
// the query string always makes it private.
func isAPIPublic(acl *types.FinalACL, path string, r *http.Request) bool {
	public := false
	if acl != nil {
		api := acl.API(path)
		if acl.Access {
			if api != nil && !api.Access {
				public = true
			}
		} else if api == nil || !api.Access {
			public = true
		}
		if acl.APIsPermission == "restricted" && api != nil && !api.Access {
			public = true
		}
	}
	if public && hasAccessToken(r) {
		public = false
	}
	return public
}

func hasAccessToken(r *http.Request) bool {
	soajsCtx := GetSOAJSContext(r.Context())
	if soajsCtx != nil && soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		return soajsCtx.Controller.ServiceParams.AccessToken != ""
	}
	return r.URL.Query().Get("access_token") != ""
}

// geoAllowed applies the extKey's geo deny and allow lists (CIDR blocks or addresses) to the
// client IP.
func geoAllowed(geo map[string]interface{}, ip string) bool {
	if geo == nil || ip == "" {
		return true
	}
	if deny, ok := geo["deny"].([]interface{}); ok && matchesAny(deny, ip) {
		return false
	}
	if allow, ok := geo["allow"].([]interface{}); ok && !matchesAny(allow, ip) {
		return false
	}
	return true
}

func matchesAny(list []interface{}, ip string) bool {
	for _, e := range list {
		if s, ok := e.(string); ok && matchIPPattern(ip, s) {
			return true
		}
	}
	return false
}

// versionFlags are the registry flags of a service version.
func versionFlags(reg registry.Registry, service, version string) types.VersionInfo {
	if reg == nil {
		return types.VersionInfo{}
	}
	data := reg.Get()
	if data == nil {
		return types.VersionInfo{}
	}
	svc := data.Services[service]
	if svc == nil || svc.Versions[version] == nil {
		return types.VersionInfo{}
	}
	return *svc.Versions[version]
}

// UserACL loads the token's user from urac after OAuth. The user's ACL override (Node.js
// aclUrackCheck) and the urac checks (uracCheck) come with the urac rebuild; the Go-only refusals
// that were here answered with codes Node.js does not use.
func UserACL() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip /soajs/* routes - they're handled by SoajsRoutes middleware
			path := r.URL.Path
			if strings.HasPrefix(path, "/soajs/") {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// Skip if no user info or no URAC driver
			if soajsCtx == nil || soajsCtx.Controller == nil ||
				soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.UserInfo == nil ||
				soajsCtx.Controller.ServiceParams.UracDriver == nil {
				next.ServeHTTP(w, r)
				return
			}

			params := soajsCtx.Controller.ServiceParams

			// Get user ID from OAuth token
			userID, ok := params.UserInfo["_id"].(string)
			if !ok || userID == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Get tenant info first
			tenantID := soajsCtx.Tenant.ID
			tenantCode := soajsCtx.Tenant.Code

			// Get user from URAC (with groupsConfig populated from groups for the current tenant)
			uracDriver := params.UracDriver.(*urac.Driver)
			user, err := uracDriver.GetUserWithGroupsConfigForTenant(ctx, userID, tenantID)
			if err != nil {
				soajsCtx.Log.Debug("Failed to get user from URAC")
				next.ServeHTTP(w, r)
				return
			}
			access := uracDriver.CheckTenantAccess(user, tenantID, tenantCode, "", nil)

			// Store user and access info
			params.User = user
			params.TenantAccess = access

			next.ServeHTTP(w, r)
		})
	}
}
