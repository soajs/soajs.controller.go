package middleware

import (
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// MT (Multi-Tenant) middleware handles tenant resolution and ACL enforcement
func MT(prov provision.Provision, reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// If no key, skip MT processing
			if soajsCtx == nil || soajsCtx.Key == nil {
				next.ServeHTTP(w, r)
				return
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

				// Merge ACLs: package ACL + key ACL
				finalACL := mergeACL(pkg.ACL, soajsCtx.Key.ACL, serviceName)

				if !checkServiceAccess(finalACL, serviceName, r.Method, serviceParams.URLInfo.ServicePath) {
					WriteError(w, http.StatusForbidden,
						config.ErrCodeForbidden,
						config.DefaultErrors[config.ErrCodeForbidden])
					return
				}

				// Store resolved ACL
				serviceParams.FinalACL = finalACL
			}

			// Check OAuth requirement
			if requiresOAuth(pkg, soajsCtx.Controller.ServiceParams) {
				// OAuth validation is handled by oauth middleware
				// Just mark that it's required
				soajsCtx.Controller.ServiceParams.RequiresOAuth = true
			}

			// Store product info
			soajsCtx.Controller.ServiceParams.Product = product
			soajsCtx.Controller.ServiceParams.Package = pkg

			next.ServeHTTP(w, r)
		})
	}
}

// mergeACL merges package ACL with key-specific ACL
func mergeACL(packageACL, keyACL map[string]*types.ServiceACL, serviceName string) *types.ServiceACL {
	var merged *types.ServiceACL

	// Start with package ACL
	if packageACL != nil {
		if svcACL, exists := packageACL[serviceName]; exists {
			merged = svcACL
		}
	}

	// Override with key ACL if present
	if keyACL != nil {
		if svcACL, exists := keyACL[serviceName]; exists {
			if merged == nil {
				merged = svcACL
			} else {
				// Merge: key ACL takes precedence
				if svcACL.Access {
					merged.Access = true
				}
				if svcACL.APIs != nil {
					if merged.APIs == nil {
						merged.APIs = make(map[string]*types.APIACL)
					}
					for k, v := range svcACL.APIs {
						merged.APIs[k] = v
					}
				}
			}
		}
	}

	return merged
}

// checkServiceAccess verifies if the request is allowed
func checkServiceAccess(acl *types.ServiceACL, serviceName, method, path string) bool {
	if acl == nil {
		return true // No ACL means open access
	}

	if !acl.Access {
		return false
	}

	// Check API-level ACL if defined
	if acl.APIs != nil {
		apiKey := method + path
		if apiACL, exists := acl.APIs[apiKey]; exists {
			return apiACL.Access
		}
		// Check path-only key
		if apiACL, exists := acl.APIs[path]; exists {
			return apiACL.Access
		}
	}

	return true
}

// requiresOAuth checks if OAuth is required for this request
func requiresOAuth(pkg *types.Package, params *types.ServiceParams) bool {
	if pkg == nil || params == nil || params.URLInfo == nil {
		return false
	}

	// Check package-level OAuth setting
	if pkg.ACL != nil {
		if svcACL, exists := pkg.ACL[params.URLInfo.ServiceName]; exists {
			if svcACL.OAuth {
				// Check API-level OAuth override
				if svcACL.APIs != nil {
					apiKey := params.URLInfo.ServicePath
					if apiACL, exists := svcACL.APIs[apiKey]; exists {
						return apiACL.OAuth
					}
				}
				return true
			}
		}
	}

	return false
}
