package lib

import (
	"net"
	"net/http"
	"strings"
)

// GetClientIP is the client IP as the Node.js gateway reads it (lib/http.js getClientIP): the
// X-Real-IP header, else the first X-Forwarded-For entry as sent, else the connection's address,
// without an IPv4-mapped "::ffff:" prefix.
func GetClientIP(r *http.Request) string {
	ip := r.Header.Get("X-Real-IP")
	if ip == "" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ip = strings.SplitN(xff, ",", 2)[0]
		}
	}
	if ip == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ip = host
	}
	if len(ip) >= 7 && strings.EqualFold(ip[:7], "::ffff:") {
		ip = ip[7:]
	}
	return ip
}

// GetScheme returns the request scheme (http or https)
func GetScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if scheme := r.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "http"
}

// GetHost returns the host from the request
func GetHost(r *http.Request) string {
	if host := r.Header.Get("X-Forwarded-Host"); host != "" {
		return host
	}
	return r.Host
}

// IsWebSocket checks if the request is a WebSocket upgrade
func IsWebSocket(r *http.Request) bool {
	connection := strings.ToLower(r.Header.Get("Connection"))
	upgrade := strings.ToLower(r.Header.Get("Upgrade"))
	return strings.Contains(connection, "upgrade") && upgrade == "websocket"
}
