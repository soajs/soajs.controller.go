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
		OriginalPath:     path,
		VersionRequested: false, // Will be set to true if version found in URL
	}

	// Remove leading slash
	path = strings.TrimPrefix(path, "/")

	// Split path into parts
	parts := strings.Split(path, "/")
	if len(parts) == 0 {
		return info
	}

	// First part is typically the service name (or service:version)
	serviceNV := parts[0]
	info.ServiceName = serviceNV

	// Check for version in /v1/ or /v1.1/ format (matches Node.js: /v[0-9]+(.[0-9]+)?/)
	if len(parts) > 1 {
		if isVersionPattern(parts[1]) {
			info.ServiceVersion = strings.TrimPrefix(parts[1], "v")
			info.VersionRequested = true
			if len(parts) > 2 {
				info.ServicePath = "/" + strings.Join(parts[2:], "/")
			} else {
				info.ServicePath = "/"
			}
		} else {
			info.ServicePath = "/" + strings.Join(parts[1:], "/")
		}
	}

	// Check for version in service:1 or service:1.1 format (matches Node.js)
	if info.ServiceVersion == "" {
		if idx := strings.Index(serviceNV, ":"); idx != -1 {
			possibleVersion := serviceNV[idx+1:]
			// Check if it's a valid version number
			if isNumericVersion(possibleVersion) {
				info.ServiceName = serviceNV[:idx]
				info.ServiceVersion = possibleVersion
				info.VersionRequested = true
			}
		}
	}

	// NOTE: Do NOT set default version here
	// The version should be resolved from ACL in the MT middleware if not requested
	// This matches Node.js behavior in mw/mt/utils.js line 233-243

	return info
}

// isNumericVersion checks if a string is a valid numeric version (1, 1.0, 1.0.0)
func isNumericVersion(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		if c >= '0' && c <= '9' {
			continue
		}
		if c == '.' && i > 0 && i < len(s)-1 {
			continue
		}
		return false
	}
	return true
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
