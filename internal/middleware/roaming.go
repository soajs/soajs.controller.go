package middleware

import (
	"encoding/json"
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"go.uber.org/zap"
)

// RoamingConfig holds roaming configuration from custom registry
type RoamingConfig struct {
	Services map[string]RoamingServiceConfig `json:"services"`
}

// RoamingServiceConfig holds service-specific roaming config
type RoamingServiceConfig struct {
	WhitelistIPs []string `json:"whitelistips"`
}

// HandleRoaming checks if roaming is allowed for the request
// Returns true if roaming is handled (success or error), false if not a roaming request
func HandleRoaming(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext, reg registry.Registry) bool {
	// Check for soajsroaming header
	roamingHeader := r.Header.Get(config.HeaderSOAJSRoaming)
	if roamingHeader == "" {
		return false
	}

	// Skip roaming for oauth/passport/login route
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		urlInfo := soajsCtx.Controller.ServiceParams.URLInfo
		if urlInfo != nil && urlInfo.ServiceName == "oauth" {
			// Check if it's passport/login path
			if urlInfo.ServicePath == "/passport/login" {
				return false
			}
		}
	}

	// Get roaming config from registry
	regData := reg.Get()
	if regData == nil {
		soajsCtx.Log.Debug("Detected soajsroaming but registry not loaded")
		WriteError(w, http.StatusForbidden, config.ErrCodeRoamingFailed, config.DefaultErrors[config.ErrCodeRoamingFailed])
		return true
	}

	roamingConfig := getRoamingConfig(regData.CustomRegistry)
	if roamingConfig == nil {
		soajsCtx.Log.Debug("Detected soajsroaming but unable to find oauth.roaming under custom registry")
		WriteError(w, http.StatusForbidden, config.ErrCodeRoamingFailed, config.DefaultErrors[config.ErrCodeRoamingFailed])
		return true
	}

	// Get service name
	serviceName := ""
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		serviceName = soajsCtx.Controller.ServiceParams.ServiceName
		if serviceName == "" && soajsCtx.Controller.ServiceParams.URLInfo != nil {
			serviceName = soajsCtx.Controller.ServiceParams.URLInfo.ServiceName
		}
	}

	// Check if service is in roaming config
	serviceRoamingConfig, ok := roamingConfig.Services[serviceName]
	if !ok {
		soajsCtx.Log.Debug("Detected soajsroaming but unable to find service under custom registry oauth.roaming.services",
			zap.String("service", serviceName))
		WriteError(w, http.StatusForbidden, config.ErrCodeRoamingFailed, config.DefaultErrors[config.ErrCodeRoamingFailed])
		return true
	}

	// Get client IP
	clientIP := getClientIP(r)

	// Check if client IP is whitelisted
	if !isIPWhitelisted(clientIP, serviceRoamingConfig.WhitelistIPs) {
		soajsCtx.Log.Debug("Detected soajsroaming but ip not in whitelistips for service under custom registry oauth.roaming.services",
			zap.String("ip", clientIP), zap.String("service", serviceName))
		WriteError(w, http.StatusForbidden, config.ErrCodeRoamingFailed, config.DefaultErrors[config.ErrCodeRoamingFailed])
		return true
	}

	// Roaming successful - return success response with soajsinjectobj header
	response := map[string]interface{}{
		"headObj": map[string]interface{}{
			"soajsinjectobj": r.Header.Get(config.HeaderSOAJSTenant),
		},
		"result": true,
		"data":   true,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
	return true
}

// getRoamingConfig extracts roaming config from custom registry
// Path: custom.oauth.value.roaming
func getRoamingConfig(customRegistry map[string]interface{}) *RoamingConfig {
	if customRegistry == nil {
		return nil
	}

	// Get oauth entry
	oauthEntry, ok := customRegistry["oauth"]
	if !ok {
		return nil
	}

	// Can be a map or a custom entry object
	var oauthValue interface{}

	switch v := oauthEntry.(type) {
	case map[string]interface{}:
		// Check if it's a custom entry with "value" field
		if val, exists := v["value"]; exists {
			oauthValue = val
		} else {
			// Direct oauth config
			oauthValue = v
		}
	default:
		return nil
	}

	// Get roaming from oauth value
	oauthMap, ok := oauthValue.(map[string]interface{})
	if !ok {
		return nil
	}

	roamingData, ok := oauthMap["roaming"]
	if !ok {
		return nil
	}

	// Convert to RoamingConfig
	roamingMap, ok := roamingData.(map[string]interface{})
	if !ok {
		return nil
	}

	roamingConfig := &RoamingConfig{
		Services: make(map[string]RoamingServiceConfig),
	}

	// Get services
	servicesData, ok := roamingMap["services"]
	if !ok {
		return roamingConfig
	}

	servicesMap, ok := servicesData.(map[string]interface{})
	if !ok {
		return roamingConfig
	}

	for serviceName, serviceData := range servicesMap {
		serviceMap, ok := serviceData.(map[string]interface{})
		if !ok {
			continue
		}

		serviceConfig := RoamingServiceConfig{}

		// Get whitelistips
		if whitelistData, ok := serviceMap["whitelistips"]; ok {
			switch wl := whitelistData.(type) {
			case []interface{}:
				for _, ip := range wl {
					if ipStr, ok := ip.(string); ok {
						serviceConfig.WhitelistIPs = append(serviceConfig.WhitelistIPs, ipStr)
					}
				}
			case []string:
				serviceConfig.WhitelistIPs = wl
			}
		}

		roamingConfig.Services[serviceName] = serviceConfig
	}

	return roamingConfig
}

// isIPWhitelisted checks if the IP is in the whitelist
func isIPWhitelisted(ip string, whitelist []string) bool {
	if whitelist == nil || len(whitelist) == 0 {
		return false
	}
	for _, whitelistedIP := range whitelist {
		if whitelistedIP == ip {
			return true
		}
	}
	return false
}
