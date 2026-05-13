package middleware

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/config"
)

// SoajsRoutes middleware handles /soajs/* routes that are served by the controller itself
func SoajsRoutes() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// Check if this is a /soajs/* route
			if !strings.HasPrefix(path, "/soajs/") {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx == nil {
				WriteError(w, http.StatusInternalServerError,
					config.ErrCodeServiceNotFound,
					"Context not initialized")
				return
			}

			// Handle specific soajs routes
			switch path {
			case "/soajs/acl":
				handleSoajsACL(w, r, soajsCtx)
				return
			case "/soajs/env":
				handleSoajsEnv(w, r, soajsCtx)
				return
			default:
				WriteError(w, http.StatusNotFound,
					config.ErrCodeServiceNotFound,
					"Unknown soajs route")
				return
			}
		})
	}
}

// handleSoajsACL handles the /soajs/acl endpoint
// Returns the ACL for the current key/user
func handleSoajsACL(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext) {
	// Check for key in headers
	if soajsCtx.Key == nil {
		WriteError(w, http.StatusForbidden, 132, "Please provide a key")
		return
	}

	// Build ACL response
	response := map[string]interface{}{
		"result": true,
	}

	// Get ACL from key/package
	var acl map[string]interface{}
	var packages []string

	// Check if we have service params with package info
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		params := soajsCtx.Controller.ServiceParams
		if params.Package != nil {
			acl = params.Package.ACL
		}
	}

	// Fall back to key ACL
	if acl == nil && soajsCtx.Key.ACL != nil {
		acl = soajsCtx.Key.ACL
	}

	// Build packages list
	if soajsCtx.Key != nil {
		packages = []string{soajsCtx.Key.PackageCode}
	}

	// Build tenant info
	var tenant map[string]interface{}
	if soajsCtx.Tenant != nil {
		tenant = map[string]interface{}{
			"id":   soajsCtx.Tenant.ID,
			"name": soajsCtx.Tenant.Name,
			"code": soajsCtx.Tenant.Code,
			"type": soajsCtx.Tenant.Type,
		}
	}

	// Build response data
	data := map[string]interface{}{
		"acl":      acl,
		"finalACL": buildFinalACL(acl),
		"packages": packages,
		"tenant":   tenant,
		"urac":     nil,
	}

	response["data"] = data

	// Write response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSoajsEnv handles the /soajs/env endpoint
func handleSoajsEnv(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext) {
	response := map[string]interface{}{
		"result": true,
		"data": map[string]interface{}{
			"env": soajsCtx.Log.Info, // placeholder
		},
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// buildFinalACL builds the simplified ACL structure
func buildFinalACL(acl map[string]interface{}) map[string]interface{} {
	if acl == nil {
		return nil
	}

	finalACL := make(map[string]interface{})

	for service, svcACL := range acl {
		if svcMap, ok := svcACL.(map[string]interface{}); ok {
			// Find the latest version
			for version, versionACL := range svcMap {
				// Skip non-version keys
				if version == "access" || version == "apisPermission" {
					continue
				}

				finalACL[service] = map[string]interface{}{
					"version": version,
				}

				// Check if apisPermission is restricted
				if vACL, ok := versionACL.(map[string]interface{}); ok {
					if apisPermission, ok := vACL["apisPermission"]; ok && apisPermission == "restricted" {
						// Build list of allowed APIs per method
						for method, methodACL := range vACL {
							if method == "access" || method == "apisPermission" {
								continue
							}
							if mACL, ok := methodACL.(map[string]interface{}); ok {
								if apis, ok := mACL["apis"].(map[string]interface{}); ok {
									apiList := make([]string, 0, len(apis))
									for api := range apis {
										apiList = append(apiList, api)
									}
									finalACL[service].(map[string]interface{})[method] = apiList
								}
							}
						}
					}
				}
				break // Only take first version found
			}
		}
	}

	return finalACL
}
