package middleware

import (
	"net"
	"net/http"
	"sync"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// IP2BanConfig holds IP ban configuration
type IP2BanConfig struct {
	Enabled   bool
	Blacklist []string
	Whitelist []string
}

// ip2BanStore holds the banned IPs (for programmatic additions)
type ip2BanStore struct {
	mu        sync.RWMutex
	blacklist map[string]bool
	whitelist map[string]bool
}

var banStore = &ip2BanStore{
	blacklist: make(map[string]bool),
	whitelist: make(map[string]bool),
}

// IP2Ban middleware checks if the request IP is banned
// Reads from custom registry: gateway.value.traffic.ip2ban
func IP2Ban() Middleware {
	return IP2BanWithRegistry(nil)
}

// IP2BanWithRegistry middleware checks if the request IP is banned with registry access
func IP2BanWithRegistry(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)

			// Check custom registry for banned IPs
			if reg != nil {
				regData := reg.Get()
				bannedIPs := getIP2BanList(regData)
				for _, bannedIP := range bannedIPs {
					if matchIP(ip, bannedIP) {
						WriteError(w, http.StatusForbidden,
							config.ErrCodeIPBanned,
							config.DefaultErrors[config.ErrCodeIPBanned])
						return
					}
				}
			}

			// Also check programmatic blacklist
			if isBanned(ip) {
				WriteError(w, http.StatusForbidden,
					config.ErrCodeIPBanned,
					config.DefaultErrors[config.ErrCodeIPBanned])
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getIP2BanList extracts banned IP list from custom registry
// Path: registry.custom.gateway.value.traffic.ip2ban
func getIP2BanList(regData *types.RegistryData) []string {
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

	traffic, ok := gatewayMap["traffic"].(map[string]interface{})
	if !ok {
		return nil
	}

	ip2banData, ok := traffic["ip2ban"]
	if !ok {
		return nil
	}

	var bannedIPs []string
	switch v := ip2banData.(type) {
	case []interface{}:
		for _, ip := range v {
			if ipStr, ok := ip.(string); ok {
				bannedIPs = append(bannedIPs, ipStr)
			}
		}
	case []string:
		bannedIPs = v
	}

	return bannedIPs
}

// matchIP checks if an IP matches a pattern (supports CIDR notation)
func matchIP(clientIP, pattern string) bool {
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

// BanIP adds an IP to the blacklist (programmatic)
func BanIP(ip string) {
	banStore.mu.Lock()
	defer banStore.mu.Unlock()
	banStore.blacklist[ip] = true
}

// UnbanIP removes an IP from the blacklist
func UnbanIP(ip string) {
	banStore.mu.Lock()
	defer banStore.mu.Unlock()
	delete(banStore.blacklist, ip)
}

// WhitelistIP adds an IP to the whitelist
func WhitelistIP(ip string) {
	banStore.mu.Lock()
	defer banStore.mu.Unlock()
	banStore.whitelist[ip] = true
}

func isBanned(ip string) bool {
	banStore.mu.RLock()
	defer banStore.mu.RUnlock()

	// Check whitelist first
	if banStore.whitelist[ip] {
		return false
	}

	return banStore.blacklist[ip]
}

func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header
	xff := r.Header.Get("X-Forwarded-For")
	if xff != "" {
		// Take the first IP in the list
		if idx := len(xff); idx > 0 {
			for i, c := range xff {
				if c == ',' {
					return xff[:i]
				}
			}
			return xff
		}
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
