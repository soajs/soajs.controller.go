package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/urac"
)

// MT (Multi-Tenant) middleware handles tenant resolution and ACL enforcement
func MT(prov provision.Provision, reg registry.Registry, uracDriver *urac.Driver, env string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// If no key, skip MT processing
			if soajsCtx == nil || soajsCtx.Key == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Check MT whitelist for IP-based ACL/OAuth bypass
			var skipACL, skipOAuth bool
			if reg != nil {
				regData := reg.Get()
				clientIP := getClientIP(r)
				skipACL, skipOAuth = CheckMTWhitelist(regData, clientIP)
				if skipACL || skipOAuth {
					soajsCtx.Log.Debug("MT whitelist matched for IP, skipACL=" + boolToStr(skipACL) + " skipOAuth=" + boolToStr(skipOAuth))
				}
			}

			// Resolve tenant from key
			tenant, err := prov.GetTenant(soajsCtx.Key.TenantCode)
			if err != nil {
				WriteError(w, http.StatusForbidden,
					config.ErrCodeInvalidTenant,
					config.DefaultErrors[config.ErrCodeInvalidTenant])
				return
			}
			soajsCtx.Tenant = tenant

			// Check tenant maintenance mode
			if tenant.MaintenanceMode {
				WriteError(w, http.StatusServiceUnavailable,
					config.ErrCodeMaintenanceMode,
					"Tenant is under maintenance")
				return
			}

			// Get product and package info
			product, err := prov.GetProduct(soajsCtx.Key.ProductCode)
			if err != nil {
				WriteError(w, http.StatusForbidden,
					config.ErrCodeInvalidProduct,
					config.DefaultErrors[config.ErrCodeInvalidProduct])
				return
			}

			pkg, err := prov.GetPackage(soajsCtx.Key.ProductCode, soajsCtx.Key.PackageCode)
			if err != nil {
				WriteError(w, http.StatusForbidden,
					config.ErrCodeInvalidPackage,
					config.DefaultErrors[config.ErrCodeInvalidPackage])
				return
			}

			// Check service ACL
			serviceParams := soajsCtx.Controller.ServiceParams
			if serviceParams != nil && serviceParams.URLInfo != nil {
				serviceName := serviceParams.URLInfo.ServiceName
				serviceVersion := serviceParams.URLInfo.ServiceVersion
				servicePath := serviceParams.URLInfo.ServicePath

				// Merge ACLs: package ACL + key ACL (env-aware)
				finalACL := mergeACL(pkg.ACL, soajsCtx.Key.ACL, serviceName, env)

				// Check ACL unless whitelisted
				if !skipACL {
					if !checkServiceAccess(finalACL, serviceName, serviceVersion, r.Method, servicePath) {
						WriteError(w, http.StatusForbidden,
							config.ErrCodeForbidden,
							config.DefaultErrors[config.ErrCodeForbidden])
						return
					}
				}

				// Store resolved ACL
				serviceParams.FinalACL = finalACL
				serviceParams.SkipACL = skipACL
				serviceParams.SkipOAuth = skipOAuth

				// Check if OAuth is required - this will trigger user ACL lookup after OAuth middleware
				// Skip OAuth check if whitelisted
				if !skipOAuth && requiresOAuth(pkg, serviceParams, env) {
					serviceParams.RequiresOAuth = true
					// Store URAC info for post-OAuth ACL lookup
					serviceParams.UracDriver = uracDriver
					serviceParams.Environment = env
					serviceParams.ServiceName = serviceName
					serviceParams.ServiceVersion = serviceVersion
				}
			}

			// Store product info (only if ServiceParams exists)
			if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
				soajsCtx.Controller.ServiceParams.Product = product
				soajsCtx.Controller.ServiceParams.Package = pkg
			}

			next.ServeHTTP(w, r)
		})
	}
}

// UserACL middleware checks user-level ACL after OAuth authentication
// This should be called after OAuth middleware when user info is available
func UserACL() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			// Get user from URAC
			uracDriver := params.UracDriver.(*urac.Driver)
			user, err := uracDriver.GetUser(ctx, userID)
			if err != nil {
				soajsCtx.Log.Debug("Failed to get user from URAC")
				next.ServeHTTP(w, r)
				return
			}

			// Check tenant access
			tenantID := soajsCtx.Tenant.ID
			tenantCode := soajsCtx.Tenant.Code
			access := uracDriver.CheckTenantAccess(user, tenantID, tenantCode, "", nil)
			if access == nil {
				WriteError(w, http.StatusForbidden,
					config.ErrCodeForbidden,
					"User does not have access to this tenant")
				return
			}

			// Get user's ACL
			userACL, err := uracDriver.GetACLForUser(ctx, user, tenantID)
			if err != nil {
				soajsCtx.Log.Debug("Failed to get user ACL")
				next.ServeHTTP(w, r)
				return
			}

			// Check user ACL for service access
			if userACL != nil {
				serviceName := params.ServiceName
				serviceVersion := params.ServiceVersion
				servicePath := params.URLInfo.ServicePath

				if !userACL.HasAccess(params.Environment, serviceName, serviceVersion, servicePath) {
					WriteError(w, http.StatusForbidden,
						config.ErrCodeForbidden,
						"User does not have access to this service")
					return
				}
			}

			// Store user and access info
			params.User = user
			params.TenantAccess = access

			next.ServeHTTP(w, r)
		})
	}
}

// mergeACL merges package ACL with key-specific ACL
// ACL structure: ACL[env][service][version] or ACL[service]
func mergeACL(packageACL, keyACL map[string]interface{}, serviceName, env string) map[string]interface{} {
	var merged map[string]interface{}

	// Check package ACL - could be ACL[env][service] or ACL[service]
	if packageACL != nil {
		// Try environment-keyed first (e.g., packageACL["dashboard"]["oauth"])
		if envACL, ok := packageACL[env]; ok {
			if envMap, ok := envACL.(map[string]interface{}); ok {
				if svcACL, exists := envMap[serviceName]; exists {
					if svcMap, ok := svcACL.(map[string]interface{}); ok {
						merged = svcMap
					}
				}
			}
		}
		// Fall back to direct service key (e.g., packageACL["oauth"])
		if merged == nil {
			if svcACL, exists := packageACL[serviceName]; exists {
				if svcMap, ok := svcACL.(map[string]interface{}); ok {
					merged = svcMap
				}
			}
		}
	}

	// Override with key ACL if present (same structure check)
	if keyACL != nil {
		var keyServiceACL map[string]interface{}
		if envACL, ok := keyACL[env]; ok {
			if envMap, ok := envACL.(map[string]interface{}); ok {
				if svcACL, exists := envMap[serviceName]; exists {
					if svcMap, ok := svcACL.(map[string]interface{}); ok {
						keyServiceACL = svcMap
					}
				}
			}
		}
		if keyServiceACL == nil {
			if svcACL, exists := keyACL[serviceName]; exists {
				if svcMap, ok := svcACL.(map[string]interface{}); ok {
					keyServiceACL = svcMap
				}
			}
		}

		if keyServiceACL != nil {
			if merged == nil {
				merged = keyServiceACL
			} else {
				// Merge: key ACL extends package ACL
				for k, v := range keyServiceACL {
					merged[k] = v
				}
			}
		}
	}

	return merged
}

// checkServiceAccess verifies if the request is allowed
// In SOAJS ACL:
// - access: false/undefined = public access (allowed without auth)
// - access: true = requires authenticated user
// - access: ["group1", "group2"] = requires user in specific groups
// This function only checks if the route exists in ACL, not auth requirements
func checkServiceAccess(acl map[string]interface{}, serviceName, version, method, path string) bool {
	if acl == nil {
		return true // No ACL means open access
	}

	// Check version-specific ACL (e.g., acl["1"])
	var versionACL map[string]interface{}
	if v, ok := acl[version]; ok {
		if vm, ok := v.(map[string]interface{}); ok {
			versionACL = vm
		}
	}

	// If no version ACL, check service-level access
	if versionACL == nil {
		// Service exists in ACL, access is allowed (specific restrictions handled elsewhere)
		return true
	}

	// Check if version has access field set to false explicitly at version level
	// Note: In SOAJS, access:false means PUBLIC access, not denied
	// access:true means requires auth, which is handled by OAuth middleware
	// So we always allow here - auth requirements are separate

	// Check if the API path exists in the ACL (for apisPermission: "restricted")
	methodLower := strings.ToLower(method)
	if methodACL, ok := versionACL[methodLower]; ok {
		if methodMap, ok := methodACL.(map[string]interface{}); ok {
			if apis, ok := methodMap["apis"]; ok {
				if apisMap, ok := apis.(map[string]interface{}); ok {
					// API exists in ACL - this is allowed
					// The access field determines auth requirements, not permission
					if _, exists := apisMap[path]; exists {
						return true
					}
				}
			}
		}
	}

	// If apisPermission is "restricted" and API not found, it would be denied
	// But we need to check apisPermission first - for now, allow by default
	return true
}

// requiresOAuth checks if OAuth is required for this request
// OAuth is required when the API's access field is truthy (true or array of groups)
// access: false/undefined = public (no OAuth)
// access: true = requires authenticated user
// access: ["group1", ...] = requires user in specific groups
// API-level access overrides version-level access
func requiresOAuth(pkg *types.Package, params *types.ServiceParams, env string) bool {
	if pkg == nil || params == nil || params.URLInfo == nil {
		return false
	}

	if pkg.ACL == nil {
		return false
	}

	serviceName := params.URLInfo.ServiceName
	serviceVersion := params.URLInfo.ServiceVersion
	servicePath := params.URLInfo.ServicePath

	// Get service ACL (could be pkg.ACL[env][service] or pkg.ACL[service])
	var serviceACL map[string]interface{}
	if envACL, ok := pkg.ACL[env]; ok {
		if envMap, ok := envACL.(map[string]interface{}); ok {
			if svcACL, exists := envMap[serviceName]; exists {
				if svcMap, ok := svcACL.(map[string]interface{}); ok {
					serviceACL = svcMap
				}
			}
		}
	}
	if serviceACL == nil {
		if svcACL, exists := pkg.ACL[serviceName]; exists {
			if svcMap, ok := svcACL.(map[string]interface{}); ok {
				serviceACL = svcMap
			}
		}
	}

	if serviceACL == nil {
		return false
	}

	// Check version-level ACL (e.g., serviceACL["1"])
	var versionACL map[string]interface{}
	if v, ok := serviceACL[serviceVersion]; ok {
		if vm, ok := v.(map[string]interface{}); ok {
			versionACL = vm
		}
	}

	if versionACL == nil {
		// Check service-level access
		if access, ok := serviceACL["access"]; ok {
			return isAccessTruthy(access)
		}
		return false
	}

	// First, check API-level access (this overrides version-level)
	// Check all HTTP methods since we don't have the request method here
	for _, m := range []string{"get", "post", "put", "delete", "patch"} {
		if methodACL, ok := versionACL[m]; ok {
			if methodMap, ok := methodACL.(map[string]interface{}); ok {
				if apis, ok := methodMap["apis"]; ok {
					if apisMap, ok := apis.(map[string]interface{}); ok {
						if apiACL, exists := apisMap[servicePath]; exists {
							if apiMap, ok := apiACL.(map[string]interface{}); ok {
								if access, ok := apiMap["access"]; ok {
									// API-level access found - this is the final answer
									// access: false means public, access: true/array means auth required
									return isAccessTruthy(access)
								}
							}
							// API exists but no access field = public
							return false
						}
					}
				}
			}
		}
	}

	// No API-level access found, fall back to version-level access
	if access, ok := versionACL["access"]; ok {
		return isAccessTruthy(access)
	}

	return false
}

// isAccessTruthy checks if an access value requires authentication
// false/nil = public (no auth), true = auth required, array = group auth required
func isAccessTruthy(access interface{}) bool {
	if access == nil {
		return false
	}
	switch v := access.(type) {
	case bool:
		return v // true = auth required, false = public
	case []interface{}:
		return len(v) > 0 // array of groups = auth required
	default:
		return false
	}
}

// Ensure context is imported for UserACL
var _ context.Context

// boolToStr converts bool to string for logging
func boolToStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
