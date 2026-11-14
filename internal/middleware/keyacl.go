package middleware

import (
	"net/http"
	"sort"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// KeyACL builds the final ACL of the request from its package ACL, as the Node.js keyACL
// middleware does (mw/keyACL/index.js): the requested service version, or the latest the package
// grants, which becomes the request's version; a requested version the package does not have
// answers 154. A service the package does not grant has no final ACL; mt refuses it.
func KeyACL() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Key == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil {
				next.ServeHTTP(w, r)
				return
			}
			params := soajsCtx.Controller.ServiceParams
			if params.Package == nil || params.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}
			serviceACL, _ := params.Package.ACL[params.URLInfo.ServiceName].(map[string]interface{})
			if serviceACL == nil {
				next.ServeHTTP(w, r)
				return
			}
			var versionACL map[string]interface{}
			if params.URLInfo.ServiceVersion != "" {
				versionACL, _ = serviceACL[sanitizeVersion(params.URLInfo.ServiceVersion)].(map[string]interface{})
				if versionACL == nil {
					WriteCode(w, 154)
					return
				}
			} else {
				latest := ""
				for v := range serviceACL {
					if latest == "" || lib.IsLatest(v, latest) {
						latest = v
					}
				}
				if latest != "" {
					params.URLInfo.ServiceVersion = unsanitizeVersion(latest)
					versionACL, _ = serviceACL[latest].(map[string]interface{})
				}
			}
			params.ACL = keyFinalACL(versionACL, strings.ToLower(r.Method))
			next.ServeHTTP(w, r)
		})
	}
}

// keyFinalACL is the final ACL of a version ACL for a method. Routes with path parameters are
// matched in route order, the last match winning, as Node.js does in document order.
func keyFinalACL(versionACL map[string]interface{}, method string) *types.FinalACL {
	if versionACL == nil {
		return nil
	}
	final := &types.FinalACL{}
	final.Access, _ = versionACL["access"].(bool)
	final.APIsPermission, _ = versionACL["apisPermission"].(string)
	methodACL, _ := versionACL[method].(map[string]interface{})
	apis, _ := methodACL["apis"].(map[string]interface{})
	routes := make([]string, 0, len(apis))
	for route := range apis {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		r, _ := apis[route].(map[string]interface{})
		api := &types.ACLAPI{Route: route}
		api.Access, _ = r["access"].(bool)
		api.Group, _ = r["group"].(string)
		if strings.Contains(route, "/:") {
			re, err := lib.PathToRegexp(route)
			if err != nil {
				continue
			}
			api.RegExp = re
			final.APIsRegExp = append(final.APIsRegExp, api)
			continue
		}
		if final.APIs == nil {
			final.APIs = map[string]*types.ACLAPI{}
		}
		final.APIs[route] = api
	}
	return final
}

// sanitizeVersion and unsanitizeVersion turn the first "." of a version into "x" and back, as
// versions are stored as keys (soajs.core.libs version).
func sanitizeVersion(v string) string   { return strings.Replace(v, ".", "x", 1) }
func unsanitizeVersion(v string) string { return strings.Replace(v, "x", ".", 1) }
