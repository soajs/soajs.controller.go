package middleware

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/urac"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.uber.org/zap"
)

// SoajsRoutesSetup middleware runs BEFORE OAuth/UserACL to configure /soajs/* routes
// This sets up UracDriver for user lookup - OAuth middleware will handle token validation
func SoajsRoutesSetup(uracDriver interface{}) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path

			// Only handle /soajs/acl setup
			if path == "/soajs/acl" {
				ctx := r.Context()
				soajsCtx := GetSOAJSContext(ctx)

				if soajsCtx != nil && soajsCtx.Controller != nil {
					// Initialize ServiceParams if not set (for special routes like /soajs/acl)
					if soajsCtx.Controller.ServiceParams == nil {
						soajsCtx.Controller.ServiceParams = &types.ServiceParams{}
					}

					// Set UracDriver for UserACL middleware to use
					soajsCtx.Controller.ServiceParams.UracDriver = uracDriver
					// Set environment for ACL resolution
					soajsCtx.Controller.ServiceParams.Environment = soajsCtx.Environment
					// Note: RequiresOAuth is not set - OAuth middleware will optionally
					// process tokens if present without requiring them
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractTokenFromRequest extracts OAuth token from request (for setup check)
func extractTokenFromRequest(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get(config.HeaderAuthorization)
	if auth != "" {
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		return auth
	}

	// Check access_token header
	if token := r.Header.Get(config.HeaderSOAJSAccessToken); token != "" {
		return token
	}

	// Check query parameter
	if token := r.URL.Query().Get("access_token"); token != "" {
		return token
	}

	return ""
}

// SoajsRoutes middleware handles /soajs/* routes that are served by the controller itself
// This runs AFTER OAuth/UserACL so user data is available
func SoajsRoutes(prov interface{}) Middleware {
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
				handleSoajsACL(w, r, soajsCtx, prov)
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
// Priority: user ACL (merged from all packages) > application ACL > single package ACL
func handleSoajsACL(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext, prov interface{}) {
	// Check for key in headers
	if soajsCtx.Key == nil {
		WriteError(w, http.StatusForbidden, 132, "Please provide a key")
		return
	}

	// Get current environment from context
	currentEnv := soajsCtx.Environment

	// Initialize response fields
	var acl map[string]interface{}
	var packages []string
	var uracInfo map[string]interface{}

	// Check if we have service params
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		params := soajsCtx.Controller.ServiceParams

		// Get product code for package lookup
		productCode := ""
		if soajsCtx.Key != nil {
			productCode = soajsCtx.Key.ProductCode
		}

		// Get user ID from OAuth token's user info
		var userID string
		if params.UserInfo != nil {
			if id, ok := params.UserInfo["_id"].(string); ok && id != "" {
				userID = id
				uracInfo = map[string]interface{}{
					"_id": userID,
				}
			}
		}

		// Priority 1: Try to get packages from OAuth token's embedded groupsConfig
		if params.UserInfo != nil {
			packages = getPackagesFromUserInfo(params.UserInfo, productCode)
		}

		// Priority 2: If no packages from token, fetch from URAC using user's groups
		// The token's user object often doesn't have groupsConfig - we need to build it from user's groups
		if len(packages) == 0 && userID != "" && params.UracDriver != nil {
			if uracDriver, ok := params.UracDriver.(*urac.Driver); ok {
				ctx := r.Context()
				// Get tenant ID from context - user may have access via AllowedTenants
				tenantID := ""
				if soajsCtx.Tenant != nil {
					tenantID = soajsCtx.Tenant.ID
				}
				soajsCtx.Log.Debug("GetAllowedPackagesForTenant called",
					zap.String("userID", userID),
					zap.String("productCode", productCode),
					zap.String("tenantID", tenantID),
					zap.String("uracDB", uracDriver.GetDatabase()),
				)
				pkgs, err := uracDriver.GetAllowedPackagesForTenant(ctx, userID, productCode, tenantID)
				if err != nil {
					soajsCtx.Log.Debug("GetAllowedPackagesForTenant error",
						zap.Error(err),
						zap.String("userID", userID),
					)
				} else if len(pkgs) > 0 {
					packages = pkgs
					soajsCtx.Log.Debug("GetAllowedPackagesForTenant success",
						zap.Int("count", len(pkgs)),
						zap.Strings("packages", pkgs),
					)
				} else {
					soajsCtx.Log.Debug("GetAllowedPackagesForTenant returned empty")
				}
			} else {
				soajsCtx.Log.Debug("UracDriver type assertion failed")
			}
		} else {
			soajsCtx.Log.Debug("Skipping GetAllowedPackagesForTenant",
				zap.Int("existingPackages", len(packages)),
				zap.String("userID", userID),
				zap.Bool("hasUracDriver", params.UracDriver != nil),
			)
		}

		// If we have packages, fetch and merge ACLs from all of them
		// This matches Node.js behavior: provision.getPackagesData + lib.mergeACLArray
		if len(packages) > 0 && prov != nil {
			if provImpl, ok := prov.(provision.Provision); ok {
				mergedACL := mergePackageACLs(provImpl, productCode, packages, currentEnv)
				if mergedACL != nil {
					acl = mergedACL
				}
			}
		}

		// Priority 2: Try to get ACL from URAC (logged-in user) if not already set
		if acl == nil && params.UracDriver != nil && params.User != nil {
			// Get user's ACL from URAC driver
			if uracDriver, ok := params.UracDriver.(interface {
				GetACLForUser(ctx interface{}, user interface{}, tenantID string) (interface{}, error)
			}); ok {
				ctx := r.Context()
				tenantID := ""
				if soajsCtx.Tenant != nil {
					tenantID = soajsCtx.Tenant.ID
				}

				if userACL, err := uracDriver.GetACLForUser(ctx, params.User, tenantID); err == nil && userACL != nil {
					// Convert GroupACL to map for response
					acl = convertGroupACLToMap(userACL, currentEnv)
				}
			}

			// Get user's allowed packages from URAC user's groupsConfig (if not already set)
			if packages == nil {
				packages = getUserAllowedPackages(params.User, productCode)
			}

			// Get user profile for urac field (if not already set)
			if uracInfo == nil {
				if userID := getUserID(params.User); userID != "" {
					uracInfo = map[string]interface{}{
						"_id": userID,
					}
				}
			}
		}

		// Priority 3: Try application ACL (from key)
		if acl == nil && soajsCtx.Key.AppACL != nil {
			acl = extractEnvACL(soajsCtx.Key.AppACL, currentEnv)
		}

		// Priority 4: Fall back to package ACL
		if acl == nil && params.Package != nil && params.Package.ACL != nil {
			acl = extractEnvACL(params.Package.ACL, currentEnv)
		}

		// Default packages from key if not set from user
		if packages == nil && soajsCtx.Key != nil && soajsCtx.Key.PackageCode != "" {
			packages = []string{soajsCtx.Key.PackageCode}
		}
	}

	// Last resort: try key's ACL directly
	if acl == nil && soajsCtx.Key.ACL != nil {
		acl = extractEnvACL(soajsCtx.Key.ACL, currentEnv)
	}

	// Default packages from key if still not set
	if packages == nil && soajsCtx.Key != nil && soajsCtx.Key.PackageCode != "" {
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

	// Build response - NO wrapper, direct response like Node.js controllerResponse
	finalACL := buildFinalACL(acl)

	response := map[string]interface{}{
		"acl":      acl,
		"finalACL": finalACL,
		"packages": packages,
		"urac":     uracInfo,
		"tenant":   tenant,
	}

	// Write response directly without any wrapper
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// convertGroupACLToMap converts a GroupACL struct to a map[string]interface{} for JSON response
func convertGroupACLToMap(groupACL interface{}, env string) map[string]interface{} {
	// Use reflection or type assertion to convert
	data, err := json.Marshal(groupACL)
	if err != nil {
		return nil
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil
	}

	// If the ACL has environments structure, extract the current env
	if envs, ok := result["environments"].(map[string]interface{}); ok {
		if envACL, ok := envs[env].(map[string]interface{}); ok {
			if services, ok := envACL["services"].(map[string]interface{}); ok {
				return services
			}
		}
	}

	return result
}

// getUserAllowedPackages extracts allowed packages from user's groupsConfig
// In Node.js, user.groupsConfig.allowedPackages[productCode] contains the packages
func getUserAllowedPackages(user interface{}, productCode string) []string {
	if user == nil || productCode == "" {
		return nil
	}

	// Try to get packages from user's groupsConfig
	data, err := json.Marshal(user)
	if err != nil {
		return nil
	}

	var userMap map[string]interface{}
	if err := json.Unmarshal(data, &userMap); err != nil {
		return nil
	}

	// Check groupsConfig.allowedPackages[productCode] (Node.js structure)
	if groupsConfig, ok := userMap["groupsConfig"].(map[string]interface{}); ok {
		if allowedPkgs, ok := groupsConfig["allowedPackages"].(map[string]interface{}); ok {
			if productPkgs, ok := allowedPkgs[productCode].([]interface{}); ok {
				var packages []string
				for _, pkg := range productPkgs {
					if pkgStr, ok := pkg.(string); ok {
						packages = append(packages, pkgStr)
					}
				}
				if len(packages) > 0 {
					return packages
				}
			}
		}
	}

	// Fallback: check config.packages (alternative structure)
	if config, ok := userMap["config"].(map[string]interface{}); ok {
		if pkgs, ok := config["packages"].(map[string]interface{}); ok {
			if productPkgs, ok := pkgs[productCode].([]interface{}); ok {
				var packages []string
				for _, pkg := range productPkgs {
					if pkgStr, ok := pkg.(string); ok {
						packages = append(packages, pkgStr)
					}
				}
				if len(packages) > 0 {
					return packages
				}
			}
		}
	}

	return nil
}

// getUserID extracts user ID from user object
func getUserID(user interface{}) string {
	data, err := json.Marshal(user)
	if err != nil {
		return ""
	}

	var userMap map[string]interface{}
	if err := json.Unmarshal(data, &userMap); err != nil {
		return ""
	}

	if id, ok := userMap["_id"].(string); ok {
		return id
	}
	return ""
}

// getPackagesFromUserInfo extracts packages from OAuth token's embedded user object
// The token contains user.groupsConfig.allowedPackages[productCode]
func getPackagesFromUserInfo(userInfo map[string]interface{}, productCode string) []string {
	if userInfo == nil || productCode == "" {
		return nil
	}

	// Check groupsConfig.allowedPackages[productCode]
	groupsConfig, ok := userInfo["groupsConfig"].(map[string]interface{})
	if !ok {
		return nil
	}

	allowedPkgs, ok := groupsConfig["allowedPackages"].(map[string]interface{})
	if !ok {
		return nil
	}

	productPkgs, ok := allowedPkgs[productCode]
	if !ok {
		return nil
	}

	// Handle both []interface{} and []string
	switch pkgs := productPkgs.(type) {
	case []interface{}:
		var packages []string
		for _, pkg := range pkgs {
			if pkgStr, ok := pkg.(string); ok {
				packages = append(packages, pkgStr)
			}
		}
		if len(packages) > 0 {
			return packages
		}
	case []string:
		if len(pkgs) > 0 {
			return pkgs
		}
	}

	return nil
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
		svcMap := toMap(svcACL)
		if svcMap == nil {
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
				if vACL := toMap(value); vACL != nil {
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
			// Only include APIs where access is true (not false)
			for method, methodACL := range latestVersionACL {
				if method == "access" || method == "apisPermission" {
					continue
				}

				mACL := toMap(methodACL)
				if mACL == nil {
					continue
				}

				apis := toMap(mACL["apis"])
				if apis == nil {
					continue
				}

				apiList := make([]string, 0, len(apis))
				for api, apiACL := range apis {
					// Check if this API has access: true
					// If access is explicitly false, skip it
					// If access is true or not specified, include it
					includeAPI := true
					if apiMap := toMap(apiACL); apiMap != nil {
						if access, hasAccess := apiMap["access"]; hasAccess {
							// access: false means skip, access: true means include
							switch v := access.(type) {
							case bool:
								if !v {
									includeAPI = false
								}
							case string:
								// Handle string "false"
								if v == "false" {
									includeAPI = false
								}
							}
						}
					}
					if includeAPI {
						apiList = append(apiList, api)
					}
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

// toMap converts various BSON types to map[string]interface{}
// Handles bson.M, bson.D, primitive.M, primitive.D, and regular maps
func toMap(v interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	// Already a map
	if m, ok := v.(map[string]interface{}); ok {
		return m
	}
	// bson.M (which is primitive.M, which is map[string]interface{})
	if m, ok := v.(bson.M); ok {
		return map[string]interface{}(m)
	}
	// bson.D (ordered document - slice of {Key, Value})
	if d, ok := v.(bson.D); ok {
		result := make(map[string]interface{})
		for _, elem := range d {
			result[elem.Key] = elem.Value
		}
		return result
	}
	// primitive.D (same as bson.D)
	if d, ok := v.(primitive.D); ok {
		result := make(map[string]interface{})
		for _, elem := range d {
			result[elem.Key] = elem.Value
		}
		return result
	}
	return nil
}

// mergePackageACLs fetches ACLs from all packages and merges them together
// This matches the Node.js behavior: provision.getPackagesData + lib.mergeACLArray
func mergePackageACLs(prov provision.Provision, productCode string, packages []string, currentEnv string) map[string]interface{} {
	if len(packages) == 0 {
		return nil
	}

	// Collect ACLs from all packages
	var packageACLs []map[string]interface{}

	for _, pkgCode := range packages {
		pkg, err := prov.GetPackage(productCode, pkgCode)
		if err != nil {
			continue
		}

		if pkg.ACL == nil {
			continue
		}

		// Extract environment-specific ACL (package ACL is keyed by env)
		envACL := extractEnvACL(pkg.ACL, currentEnv)
		if envACL != nil {
			packageACLs = append(packageACLs, envACL)
		}
	}

	if len(packageACLs) == 0 {
		return nil
	}

	// If only one ACL, return it directly
	if len(packageACLs) == 1 {
		return packageACLs[0]
	}

	// Merge all ACLs together (matching Node.js lib.mergeACLArray behavior)
	return mergeACLArray(packageACLs)
}

// mergeACLArray merges multiple ACLs into one
// This matches the Node.js lib.mergeACLArray behavior:
// - For each service: if doesn't exist, add it
// - For each version: if doesn't exist, add it
// - For restricted APIs: merge only if both are restricted
// - For non-restricted: merge only if both are non-restricted
func mergeACLArray(aclArray []map[string]interface{}) map[string]interface{} {
	if len(aclArray) == 0 {
		return nil
	}
	if len(aclArray) == 1 {
		return cloneACL(aclArray[0])
	}

	// Start with a deep copy of the first ACL
	merged := cloneACL(aclArray[0])

	// Merge each subsequent ACL
	for i := 1; i < len(aclArray); i++ {
		mergeOneACL(merged, aclArray[i])
	}

	return merged
}

// mergeOneACL merges one ACL into the target ACL (modifies target in place)
func mergeOneACL(target, source map[string]interface{}) {
	if source == nil {
		return
	}

	for serviceName, serviceValue := range source {
		serviceACL := toMap(serviceValue)
		if serviceACL == nil {
			continue
		}

		// If service doesn't exist in target, add it
		if target[serviceName] == nil {
			target[serviceName] = cloneACL(serviceACL)
			continue
		}

		targetService := toMap(target[serviceName])
		if targetService == nil {
			target[serviceName] = cloneACL(serviceACL)
			continue
		}

		// Merge versions within the service
		for versionKey, versionValue := range serviceACL {
			versionACL := toMap(versionValue)
			if versionACL == nil {
				continue
			}

			// If version doesn't exist in target, add it
			if targetService[versionKey] == nil {
				targetService[versionKey] = cloneACL(versionACL)
				target[serviceName] = targetService
				continue
			}

			targetVersion := toMap(targetService[versionKey])
			if targetVersion == nil {
				targetService[versionKey] = cloneACL(versionACL)
				target[serviceName] = targetService
				continue
			}

			// Check apisPermission for merge strategy
			sourceRestricted := isRestricted(versionACL)
			targetRestricted := isRestricted(targetVersion)

			if sourceRestricted {
				if targetRestricted {
					// Both restricted: merge them
					mergeVersionACL(targetVersion, versionACL)
					targetService[versionKey] = targetVersion
				} else {
					// Source restricted, target not: overwrite
					targetService[versionKey] = cloneACL(versionACL)
				}
			} else {
				// Source not restricted
				if targetRestricted {
					// Keep the old restricted version
					// (do nothing)
				} else {
					// Both non-restricted: merge them
					mergeVersionACL(targetVersion, versionACL)
					targetService[versionKey] = targetVersion
				}
			}
			target[serviceName] = targetService
		}
	}
}

// isRestricted checks if a version ACL has apisPermission === "restricted"
func isRestricted(versionACL map[string]interface{}) bool {
	if versionACL == nil {
		return false
	}
	apisPermission, ok := versionACL["apisPermission"].(string)
	return ok && apisPermission == "restricted"
}

// mergeVersionACL merges source version ACL into target (modifies target in place)
// This performs a deep merge of the version-level ACL
func mergeVersionACL(target, source map[string]interface{}) {
	for key, value := range source {
		if target[key] == nil {
			// Key doesn't exist in target, add it
			if srcMap := toMap(value); srcMap != nil {
				target[key] = cloneACL(srcMap)
			} else {
				target[key] = value
			}
			continue
		}

		// Key exists in both - try to deep merge if both are maps
		targetMap := toMap(target[key])
		sourceMap := toMap(value)

		if targetMap != nil && sourceMap != nil {
			// Both are maps, recursively merge
			mergeVersionACL(targetMap, sourceMap)
			target[key] = targetMap
		} else {
			// One or both are not maps, source wins
			if sourceMap != nil {
				target[key] = cloneACL(sourceMap)
			} else {
				target[key] = value
			}
		}
	}
}

