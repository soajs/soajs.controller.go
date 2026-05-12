package middleware

import (
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

// URL middleware parses and normalizes the request URL
func URL() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx != nil && soajsCtx.Controller != nil {
				// Parse URL to extract service info
				urlInfo := parseURL(r.URL.Path)
				soajsCtx.Controller.ServiceParams.URLInfo = urlInfo
			}

			next.ServeHTTP(w, r)
		})
	}
}

func parseURL(path string) *types.URLInfo {
	info := &types.URLInfo{
		OriginalPath: path,
	}

	// Remove leading slash
	path = strings.TrimPrefix(path, "/")

	// Split path into parts
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return info
	}

	// First part is typically the service name
	info.ServiceName = parts[0]

	// Check for version pattern (v1, v2, etc.)
	if len(parts) > 1 {
		if isVersionPattern(parts[1]) {
			info.ServiceVersion = strings.TrimPrefix(parts[1], "v")
			if len(parts) > 2 {
				info.ServicePath = "/" + strings.Join(parts[2:], "/")
			}
		} else {
			info.ServicePath = "/" + strings.Join(parts[1:], "/")
		}
	}

	// Default version if not specified
	if info.ServiceVersion == "" {
		info.ServiceVersion = "1"
	}

	return info
}

func isVersionPattern(s string) bool {
	if len(s) < 2 {
		return false
	}
	if s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
