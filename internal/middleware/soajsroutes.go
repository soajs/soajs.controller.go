package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"
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
			case "/soajs/proxy":
				// /soajs/proxy is handled by gotoService middleware
				// It extracts proxyRoute query param and routes to that service
				// Continue to next handler (gotoService will handle it)
				next.ServeHTTP(w, r)
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

	// Get current environment
	var currentEnv string
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		currentEnv = soajsCtx.Controller.ServiceParams.Environment
	}

	// Get ACL - priority: user ACL > application ACL > package ACL
	var acl map[string]interface{}
	var packages []string

	// Check if we have service params with package info
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		params := soajsCtx.Controller.ServiceParams

		// Get package code for packages array
		if soajsCtx.Key != nil && soajsCtx.Key.PackageCode != "" {
			packages = []string{soajsCtx.Key.PackageCode}
		}

		// Try to get ACL from user (via URAC) first - highest priority
		if params.User != nil {
			// User ACL would be set here if we have URAC integration
			// For now, fall through to other sources
		}

		// Try application ACL (from key) - already environment-specific
		if acl == nil && soajsCtx.Key.AppACL != nil {
			acl = soajsCtx.Key.AppACL
		}

		// Fall back to package ACL - need to extract environment-specific ACL
		if acl == nil && params.Package != nil && params.Package.ACL != nil {
			// Package ACL is structured as ACL[env][service][version]
			// We need to extract ACL[currentEnv]
			acl = extractEnvACL(params.Package.ACL, currentEnv)
		}
	}

	// If still no ACL, try key's ACL directly
	if acl == nil && soajsCtx.Key.ACL != nil {
		// Key ACL might also be environment-keyed
		acl = extractEnvACL(soajsCtx.Key.ACL, currentEnv)
	}

	// Build urac info
	var urac map[string]interface{}
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		params := soajsCtx.Controller.ServiceParams
		if params.UserInfo != nil {
			if userID, ok := params.UserInfo["_id"]; ok {
				urac = map[string]interface{}{
					"_id": userID,
				}
			}
		}
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

	// Build response - NO wrapper, direct response like Node.js controllerResponse
	response := map[string]interface{}{
		"acl":      acl,
		"finalACL": buildFinalACL(acl),
		"packages": packages,
		"urac":     urac,
		"tenant":   tenant,
	}

	// Write response directly without any wrapper
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// extractEnvACL extracts environment-specific ACL from a potentially environment-keyed ACL
// If the ACL is keyed by environment (e.g., {"dashboard": {"oauth": ...}}), extract the env's ACL
// If not environment-keyed (e.g., {"oauth": ...}), return as-is
func extractEnvACL(acl map[string]interface{}, env string) map[string]interface{} {
	if acl == nil || env == "" {
		return acl
	}

	// Check if this ACL is environment-keyed
	// Environment-keyed ACL: {"dashboard": {"oauth": {"1": {...}}}}
	// Non-environment-keyed: {"oauth": {"1": {...}}}

	// If the first level contains the environment key, extract it
	if envACL, ok := acl[env]; ok {
		if envMap, ok := envACL.(map[string]interface{}); ok {
			// Verify it looks like a service ACL (has service names as keys)
			// Service ACL keys are like "oauth", "urac", etc. with version numbers inside
			for _, v := range envMap {
				if serviceMap, ok := v.(map[string]interface{}); ok {
					// Check if this has version keys (numbers) or method keys
					for k := range serviceMap {
						if _, err := strconv.Atoi(k); err == nil {
							// Found a version number, this is environment-keyed
							return envMap
						}
					}
				}
			}
			// Looks like environment-keyed even if no version found
			return envMap
		}
	}

	// Check if this is already a non-environment-keyed ACL
	// by checking if top-level keys look like service names with versions
	for _, v := range acl {
		if serviceMap, ok := v.(map[string]interface{}); ok {
			for k := range serviceMap {
				if _, err := strconv.Atoi(k); err == nil {
					// Found a version number at second level, not environment-keyed
					return acl
				}
			}
		}
	}

	return acl
}

// cloneACL creates a deep copy of an ACL map
func cloneACL(acl map[string]interface{}) map[string]interface{} {
	if acl == nil {
		return nil
	}

	// Use JSON marshaling for deep copy
	data, err := json.Marshal(acl)
	if err != nil {
		return nil
	}

	var cloned map[string]interface{}
	if err := json.Unmarshal(data, &cloned); err != nil {
		return nil
	}

	return cloned
}

// buildFinalACL builds the simplified ACL structure
// Iterates through all versions and picks the latest
func buildFinalACL(acl map[string]interface{}) map[string]interface{} {
	if acl == nil {
		return nil
	}

	finalACL := make(map[string]interface{})

	for service, svcACL := range acl {
		svcMap, ok := svcACL.(map[string]interface{})
		if !ok {
			continue
		}

		var latestVersion string
		var latestVersionACL map[string]interface{}

		// Iterate through all keys to find version numbers
		for key, value := range svcMap {
			// Skip non-version keys
			if key == "access" || key == "apisPermission" {
				continue
			}

			// Check if this key is a version number
			if _, err := strconv.Atoi(key); err == nil {
				// It's a version number
				if vACL, ok := value.(map[string]interface{}); ok {
					if latestVersion == "" || compareVersions(key, latestVersion) > 0 {
						latestVersion = key
						latestVersionACL = vACL
					}
				}
			}
		}

		if latestVersion == "" || latestVersionACL == nil {
			continue
		}

		// Build final ACL for this service
		serviceFinalACL := map[string]interface{}{
			"version": latestVersion,
		}

		// Check if apisPermission is restricted
		if apisPermission, ok := latestVersionACL["apisPermission"]; ok && apisPermission == "restricted" {
			// Build list of allowed APIs per method
			for method, methodACL := range latestVersionACL {
				if method == "access" || method == "apisPermission" {
					continue
				}

				mACL, ok := methodACL.(map[string]interface{})
				if !ok {
					continue
				}

				apis, ok := mACL["apis"].(map[string]interface{})
				if !ok {
					continue
				}

				apiList := make([]string, 0, len(apis))
				for api := range apis {
					apiList = append(apiList, api)
				}

				if len(apiList) > 0 {
					serviceFinalACL[method] = apiList
				}
			}
		}

		finalACL[service] = serviceFinalACL
	}

	return finalACL
}

// compareVersions compares two version strings (simple numeric comparison)
// Returns: 1 if v1 > v2, -1 if v1 < v2, 0 if equal
func compareVersions(v1, v2 string) int {
	n1, err1 := strconv.Atoi(v1)
	n2, err2 := strconv.Atoi(v2)

	if err1 != nil || err2 != nil {
		// Fallback to string comparison
		if v1 > v2 {
			return 1
		} else if v1 < v2 {
			return -1
		}
		return 0
	}

	if n1 > n2 {
		return 1
	} else if n1 < n2 {
		return -1
	}
	return 0
}
