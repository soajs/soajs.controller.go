package middleware

import (
	"net"
	"net/http"
	"sync"

	"github.com/soajs/soajs.controller.go/internal/config"
)

// IP2BanConfig holds IP ban configuration
type IP2BanConfig struct {
	Enabled   bool
	Blacklist []string
	Whitelist []string
}

// ip2BanStore holds the banned IPs
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
func IP2Ban() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)

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

// BanIP adds an IP to the blacklist
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
