package middleware

import (
	"net"
	"regexp"
	"strings"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

// MTWhitelistConfig holds MT whitelist configuration
// Path: registry.custom.gateway.value.mt.whitelist
type MTWhitelistConfig struct {
	IPs   []string `json:"ips"`
	ACL   bool     `json:"acl"`
	OAuth bool     `json:"oauth"`
}

// PinWrapperConfig holds PIN wrapper configuration
// Path: registry.custom.oauth.value.pinWrapper
type PinWrapperConfig struct {
	ServiceName string `json:"servicename"`
	APIName     string `json:"apiname"`
}

// PinWhitelistEntry holds PIN whitelist entry for a method
type PinWhitelistEntry struct {
	APIs  []string `json:"apis"`
	Regex []string `json:"regex"`
}

// PinWhitelistConfig holds PIN whitelist per service
// Path: registry.custom.oauth.value.pinWhitelist
type PinWhitelistConfig map[string]map[string]*PinWhitelistEntry

// CheckMTWhitelist checks if the client IP is in the MT whitelist
// Returns (skipACL, skipOAuth)
func CheckMTWhitelist(regData *types.RegistryData, clientIP string) (bool, bool) {
	if regData == nil || regData.CustomRegistry == nil {
		return false, false
	}

	config := getMTWhitelistConfig(regData)
	if config == nil || len(config.IPs) == 0 {
		return false, false
	}

	// Check if client IP matches any whitelisted IP/CIDR
	for _, ipPattern := range config.IPs {
		if matchIPPattern(clientIP, ipPattern) {
			return config.ACL, config.OAuth
		}
	}

	return false, false
}

// getMTWhitelistConfig extracts MT whitelist config from custom registry
func getMTWhitelistConfig(regData *types.RegistryData) *MTWhitelistConfig {
	if regData == nil || regData.CustomRegistry == nil {
		return nil
	}

	gateway, ok := regData.CustomRegistry["gateway"]
	if !ok {
		return nil
	}

	var gatewayValue interface{}
	switch v := gateway.(type) {
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

	mt, ok := gatewayMap["mt"].(map[string]interface{})
	if !ok {
		return nil
	}

	whitelist, ok := mt["whitelist"].(map[string]interface{})
	if !ok {
		return nil
	}

	config := &MTWhitelistConfig{}

	if ips, ok := whitelist["ips"]; ok {
		switch v := ips.(type) {
		case []interface{}:
			for _, ip := range v {
				if ipStr, ok := ip.(string); ok {
					config.IPs = append(config.IPs, ipStr)
				}
			}
		case []string:
			config.IPs = v
		}
	}

	if acl, ok := whitelist["acl"].(bool); ok {
		config.ACL = acl
	}
	if oauth, ok := whitelist["oauth"].(bool); ok {
		config.OAuth = oauth
	}

	return config
}

// GetPinWrapperConfig extracts PIN wrapper config from custom registry
func GetPinWrapperConfig(regData *types.RegistryData) *PinWrapperConfig {
	if regData == nil || regData.CustomRegistry == nil {
		return nil
	}

	oauth, ok := regData.CustomRegistry["oauth"]
	if !ok {
		return nil
	}

	var oauthValue interface{}
	switch v := oauth.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			oauthValue = val
		} else {
			oauthValue = v
		}
	default:
		return nil
	}

	oauthMap, ok := oauthValue.(map[string]interface{})
	if !ok {
		return nil
	}

	pinWrapper, ok := oauthMap["pinWrapper"].(map[string]interface{})
	if !ok {
		return nil
	}

	config := &PinWrapperConfig{}
	if svc, ok := pinWrapper["servicename"].(string); ok {
		config.ServiceName = svc
	}
	if api, ok := pinWrapper["apiname"].(string); ok {
		config.APIName = api
	}

	return config
}

// GetPinWhitelistConfig extracts PIN whitelist config from custom registry
func GetPinWhitelistConfig(regData *types.RegistryData) PinWhitelistConfig {
	if regData == nil || regData.CustomRegistry == nil {
		return nil
	}

	oauth, ok := regData.CustomRegistry["oauth"]
	if !ok {
		return nil
	}

	var oauthValue interface{}
	switch v := oauth.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			oauthValue = val
		} else {
			oauthValue = v
		}
	default:
		return nil
	}

	oauthMap, ok := oauthValue.(map[string]interface{})
	if !ok {
		return nil
	}

	pinWhitelist, ok := oauthMap["pinWhitelist"].(map[string]interface{})
	if !ok {
		return nil
	}

	config := make(PinWhitelistConfig)

	for serviceName, serviceData := range pinWhitelist {
		serviceMap, ok := serviceData.(map[string]interface{})
		if !ok {
			continue
		}

		config[serviceName] = make(map[string]*PinWhitelistEntry)

		for method, methodData := range serviceMap {
			methodMap, ok := methodData.(map[string]interface{})
			if !ok {
				continue
			}

			entry := &PinWhitelistEntry{}

			if apis, ok := methodMap["apis"]; ok {
				switch v := apis.(type) {
				case []interface{}:
					for _, api := range v {
						if apiStr, ok := api.(string); ok {
							entry.APIs = append(entry.APIs, apiStr)
						}
					}
				case []string:
					entry.APIs = v
				}
			}

			if regex, ok := methodMap["regex"]; ok {
				switch v := regex.(type) {
				case []interface{}:
					for _, r := range v {
						if rStr, ok := r.(string); ok {
							entry.Regex = append(entry.Regex, rStr)
						}
					}
				case []string:
					entry.Regex = v
				}
			}

			config[serviceName][strings.ToLower(method)] = entry
		}
	}

	return config
}

// IsPinWhitelisted checks if the service/method/path is in the PIN whitelist
func IsPinWhitelisted(config PinWhitelistConfig, serviceName, method, path string) bool {
	if config == nil {
		return false
	}

	serviceConfig, ok := config[serviceName]
	if !ok {
		return false
	}

	methodConfig, ok := serviceConfig[strings.ToLower(method)]
	if !ok {
		return false
	}

	// Check exact API match
	for _, api := range methodConfig.APIs {
		if api == path {
			return true
		}
	}

	// Check regex match
	for _, pattern := range methodConfig.Regex {
		re, err := regexp.Compile(pattern)
		if err != nil {
			continue
		}
		if re.MatchString(path) {
			return true
		}
	}

	return false
}

// IsPinWrapperRoute checks if the request is for the PIN wrapper API
func IsPinWrapperRoute(config *PinWrapperConfig, serviceName, path string) bool {
	if config == nil {
		return false
	}
	return config.ServiceName == serviceName && config.APIName == path
}

// matchIPPattern checks if an IP matches a pattern (exact or CIDR)
func matchIPPattern(clientIP, pattern string) bool {
	// Exact match
	if clientIP == pattern {
		return true
	}

	// CIDR match
	_, cidr, err := net.ParseCIDR(pattern)
	if err == nil {
		ip := net.ParseIP(clientIP)
		if ip != nil && cidr.Contains(ip) {
			return true
		}
	}

	return false
}
