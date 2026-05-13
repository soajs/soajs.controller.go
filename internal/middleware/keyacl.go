package middleware

import (
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/config"
)

// KeyACL middleware validates key-based access control
func KeyACL() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// If no key, skip ACL check
			if soajsCtx == nil || soajsCtx.Key == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Get service info from URL
			serviceParams := soajsCtx.Controller.ServiceParams
			if serviceParams == nil || serviceParams.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}

			serviceName := serviceParams.URLInfo.ServiceName

			// Check if key has access to this service
			keyACL := soajsCtx.Key.ACL
			if keyACL != nil {
				// Check service-level ACL (could be direct or nested by env)
				if serviceACL, exists := keyACL[serviceName]; exists {
					// Check if access is explicitly denied
					if svcMap, ok := serviceACL.(map[string]interface{}); ok {
						if access, hasAccess := svcMap["access"]; hasAccess {
							if b, ok := access.(bool); ok && !b {
								WriteError(w, http.StatusForbidden,
									config.ErrCodeForbidden,
									config.DefaultErrors[config.ErrCodeForbidden])
								return
							}
						}
						// Store service ACL for later use
						soajsCtx.Controller.ServiceParams.KeyACL = svcMap
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
