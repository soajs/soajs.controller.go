package lib

import (
	"net/http"
	"strings"
)

// CopyHeaders copies headers from src to dst
func CopyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// CopyHeadersExcept copies headers except those in the exclude list
func CopyHeadersExcept(dst, src http.Header, exclude []string) {
	excludeMap := make(map[string]bool, len(exclude))
	for _, e := range exclude {
		excludeMap[strings.ToLower(e)] = true
	}

	for k, vv := range src {
		if excludeMap[strings.ToLower(k)] {
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

// HopHeaders are headers that should not be forwarded
var HopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailers",
	"Transfer-Encoding",
	"Upgrade",
}

// RemoveHopHeaders removes hop-by-hop headers
func RemoveHopHeaders(h http.Header) {
	for _, hop := range HopHeaders {
		h.Del(hop)
	}

	// Remove headers listed in Connection header
	if conn := h.Get("Connection"); conn != "" {
		for _, f := range strings.Split(conn, ",") {
			h.Del(strings.TrimSpace(f))
		}
	}
}

// SetForwardedHeaders sets X-Forwarded-* headers
func SetForwardedHeaders(dst http.Header, r *http.Request) {
	clientIP := GetClientIP(r)

	// X-Forwarded-For
	if prior := dst.Get("X-Forwarded-For"); prior != "" {
		clientIP = prior + ", " + clientIP
	}
	dst.Set("X-Forwarded-For", clientIP)

	// X-Forwarded-Host
	if dst.Get("X-Forwarded-Host") == "" {
		dst.Set("X-Forwarded-Host", r.Host)
	}

	// X-Forwarded-Proto
	if dst.Get("X-Forwarded-Proto") == "" {
		dst.Set("X-Forwarded-Proto", GetScheme(r))
	}
}
