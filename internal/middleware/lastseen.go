package middleware

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"go.uber.org/zap"
)

// LastSeenConfig holds lastSeen configuration from custom registry
type LastSeenConfig struct {
	Active         bool                   `json:"active"`
	ServiceName    string                 `json:"serviceName"`
	ServiceVersion string                 `json:"serviceVersion"`
	API            string                 `json:"api"`
	Network        string                 `json:"network"`
	Include        map[string]interface{} `json:"include"`
}

// regexpCache caches compiled regex patterns for route matching
var (
	regexpCache   = make(map[string]*regexp.Regexp)
	regexpCacheMu sync.RWMutex
	maxCacheSize  = 100
)

// LastSeen middleware updates user's last seen timestamp after request completion
func LastSeen(reg registry.Registry, aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Call next first - lastSeen runs after response
			next.ServeHTTP(w, r)

			// Run lastSeen check asynchronously
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)
			if soajsCtx == nil {
				return
			}

			// Check for URAC driver (user must be authenticated)
			if soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil {
				return
			}
			if soajsCtx.Controller.ServiceParams.UracDriver == nil {
				return
			}

			// Get lastSeen config from custom registry
			regData := reg.Get()
			if regData == nil {
				return
			}

			lastSeenConfig := getLastSeenConfig(regData.CustomRegistry)
			if lastSeenConfig == nil || !lastSeenConfig.Active {
				return
			}

			// Get service info
			serviceName := soajsCtx.Controller.ServiceParams.ServiceName
			if serviceName == "" && soajsCtx.Controller.ServiceParams.URLInfo != nil {
				serviceName = soajsCtx.Controller.ServiceParams.URLInfo.ServiceName
			}
			apiPath := ""
			if soajsCtx.Controller.ServiceParams.URLInfo != nil {
				apiPath = soajsCtx.Controller.ServiceParams.URLInfo.ServicePath
			}
			method := r.Method

			// Check include filter
			if !shouldTriggerLastSeenCheck(lastSeenConfig.Include, serviceName, apiPath, method) {
				soajsCtx.Log.Debug("lastSeen skipped",
					zap.String("service", serviceName),
					zap.String("method", method),
					zap.String("api", apiPath))
				return
			}

			// Get user ID from UserInfo (set by OAuth middleware)
			userId := ""
			if soajsCtx.Controller.ServiceParams.UserInfo != nil {
				if id, ok := soajsCtx.Controller.ServiceParams.UserInfo["_id"].(string); ok {
					userId = id
				}
			}
			if userId == "" {
				soajsCtx.Log.Debug("lastSeen skipped - no userId")
				return
			}

			// Make async call to update last seen
			go updateLastSeenAsync(soajsCtx.Log, reg, aw, lastSeenConfig, r.Header.Get("soajsinjectobj"))
		})
	}
}

// getLastSeenConfig extracts lastSeen config from custom registry
func getLastSeenConfig(customRegistry map[string]interface{}) *LastSeenConfig {
	if customRegistry == nil {
		return nil
	}

	// Get gateway entry
	gatewayEntry, ok := customRegistry["gateway"]
	if !ok {
		return nil
	}

	var gatewayValue interface{}
	switch v := gatewayEntry.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			gatewayValue = val
		} else {
			gatewayValue = v
		}
	default:
		return nil
	}

	gatewayMap, ok := gatewayValue.(map[string]interface{})
	if !ok {
		return nil
	}

	lastSeenData, ok := gatewayMap["lastSeen"]
	if !ok {
		return nil
	}

	lastSeenMap, ok := lastSeenData.(map[string]interface{})
	if !ok {
		return nil
	}

	config := &LastSeenConfig{
		ServiceName:    "urac",
		ServiceVersion: "3",
		API:            "/user/last/seen",
	}

	if active, ok := lastSeenMap["active"].(bool); ok {
		config.Active = active
	}
	if sn, ok := lastSeenMap["serviceName"].(string); ok && sn != "" {
		config.ServiceName = sn
	}
	if sv, ok := lastSeenMap["serviceVersion"].(string); ok && sv != "" {
		config.ServiceVersion = sv
	}
	if api, ok := lastSeenMap["api"].(string); ok && api != "" {
		config.API = api
	}
	if network, ok := lastSeenMap["network"].(string); ok {
		config.Network = network
	}
	if include, ok := lastSeenMap["include"].(map[string]interface{}); ok {
		config.Include = include
	}

	return config
}

// shouldTriggerLastSeenCheck checks if lastSeen should be triggered
func shouldTriggerLastSeenCheck(include map[string]interface{}, serviceName, apiPath, method string) bool {
	// No include filter = trigger for all (backward compatible)
	if include == nil {
		return true
	}

	// Service not in whitelist
	serviceConfig, ok := include[serviceName]
	if !ok {
		return false
	}

	// Service-level match (all APIs, all methods)
	if serviceBool, ok := serviceConfig.(bool); ok && serviceBool {
		return true
	}

	// API-level match
	serviceMap, ok := serviceConfig.(map[string]interface{})
	if !ok {
		return false
	}

	apisConfig, ok := serviceMap["apis"]
	if !ok {
		return false
	}

	apis, ok := apisConfig.(map[string]interface{})
	if !ok {
		return false
	}

	return isLastSeenApiMethodMatch(apis, apiPath, method)
}

// isLastSeenApiMethodMatch checks if API path and method match
func isLastSeenApiMethodMatch(apis map[string]interface{}, apiPath, method string) bool {
	// Check for wildcard first
	if methodConfig, ok := apis["*"]; ok {
		return isLastSeenMethodMatch(methodConfig, method)
	}

	// Check exact match
	if methodConfig, ok := apis[apiPath]; ok {
		return isLastSeenMethodMatch(methodConfig, method)
	}

	// Check pattern matching for routes with path params
	for routePattern, methodConfig := range apis {
		if strings.Contains(routePattern, "/:") {
			re := getLastSeenRouteRegExp(routePattern)
			if re != nil && re.MatchString(apiPath) {
				return isLastSeenMethodMatch(methodConfig, method)
			}
		}
	}

	return false
}

// isLastSeenMethodMatch checks if method matches
func isLastSeenMethodMatch(methodConfig interface{}, method string) bool {
	// true = all methods
	if boolVal, ok := methodConfig.(bool); ok && boolVal {
		return true
	}

	// Array of methods
	if methods, ok := methodConfig.([]interface{}); ok {
		lowerMethod := strings.ToLower(method)
		for _, m := range methods {
			if mStr, ok := m.(string); ok && strings.ToLower(mStr) == lowerMethod {
				return true
			}
		}
	}

	return false
}

// getLastSeenRouteRegExp gets or creates a cached RegExp
func getLastSeenRouteRegExp(route string) *regexp.Regexp {
	regexpCacheMu.RLock()
	if re, ok := regexpCache[route]; ok {
		regexpCacheMu.RUnlock()
		return re
	}
	regexpCacheMu.RUnlock()

	// Convert Express-style route to regex
	pattern := "^" + regexp.QuoteMeta(route)
	pattern = strings.ReplaceAll(pattern, "\\:", ":")
	re := regexp.MustCompile(`:(\w+)`)
	pattern = re.ReplaceAllString(pattern, `[^/]+`)
	pattern += "$"

	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}

	regexpCacheMu.Lock()
	if len(regexpCache) >= maxCacheSize {
		count := 0
		for k := range regexpCache {
			delete(regexpCache, k)
			count++
			if count >= maxCacheSize/4 {
				break
			}
		}
	}
	regexpCache[route] = compiled
	regexpCacheMu.Unlock()

	return compiled
}

// updateLastSeenAsync makes async call to update last seen
func updateLastSeenAsync(log *zap.Logger, reg registry.Registry, aw awareness.Awareness, config *LastSeenConfig, injectObj string) {
	// Get host for the service
	hostInfo, err := aw.GetHost(config.ServiceName, config.ServiceVersion)
	if err != nil || hostInfo == nil {
		log.Debug("lastSeen failed - no host",
			zap.String("service", config.ServiceName),
			zap.String("version", config.ServiceVersion))
		return
	}

	// Get port from registry
	regData := reg.Get()
	port := hostInfo.Port
	if regData != nil && regData.Services != nil {
		if svc, ok := regData.Services[config.ServiceName]; ok && svc.Port > 0 {
			port = svc.Port
		}
	}

	// Build request
	uri := fmt.Sprintf("http://%s:%d%s", hostInfo.Host, port, config.API)

	var body []byte
	if config.Network != "" {
		bodyMap := map[string]string{"network": config.Network}
		body, _ = json.Marshal(bodyMap)
	}

	req, err := http.NewRequest("POST", uri, bytes.NewReader(body))
	if err != nil {
		log.Debug("lastSeen failed - request creation error", zap.Error(err))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	if injectObj != "" {
		req.Header.Set("soajsinjectobj", injectObj)
	}

	// Execute request
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Debug("lastSeen failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	log.Debug("lastSeen updated", zap.Int("status", resp.StatusCode))
}
