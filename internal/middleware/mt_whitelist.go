package middleware

import (
	"net"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

// MTWhitelistConfig holds MT whitelist configuration
// Path: registry.custom.gateway.value.mt.whitelist
type MTWhitelistConfig struct {
	IPs   []string `json:"ips"`
	ACL   bool     `json:"acl"`
	OAuth bool     `json:"oauth"`
}

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

	gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
	gatewayMap, _ := gateway["value"].(map[string]interface{})
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

	// truthiness, as Node.js reads them (mw/mt/utils.js:285-291)
	config.ACL = truthy(whitelist["acl"])
	config.OAuth = truthy(whitelist["oauth"])

	return config
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
