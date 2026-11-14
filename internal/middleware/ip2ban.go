package middleware

import (
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// IP2BanWithRegistry answers 403 {"status":403,"msg":"banned"} when the client IP is one of the
// custom registry gateway value.traffic.ip2ban, matched exactly (Node.js mw/ip2ban/index.js).
func IP2BanWithRegistry(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := getClientIP(r)
			for _, banned := range getIP2BanList(reg.Get()) {
				if banned == ip {
					WriteStatusMsg(w, http.StatusForbidden, "banned", nil, false)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// getIP2BanList is the custom registry gateway value.traffic.ip2ban.
func getIP2BanList(regData *types.RegistryData) []string {
	if regData == nil {
		return nil
	}
	gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	traffic, _ := value["traffic"].(map[string]interface{})
	list, _ := traffic["ip2ban"].([]interface{})
	var banned []string
	for _, ip := range list {
		if s, ok := ip.(string); ok {
			banned = append(banned, s)
		}
	}
	return banned
}

func getClientIP(r *http.Request) string {
	return lib.GetClientIP(r)
}
