package lib

import (
	"net/url"
	"strings"
)

// ParseServiceURL parses a URL and extracts service information
func ParseServiceURL(rawURL string) (*ServiceURLInfo, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}

	info := &ServiceURLInfo{
		OriginalPath: u.Path,
		Query:        u.RawQuery,
	}

	// Remove leading slash and split
	path := strings.TrimPrefix(u.Path, "/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 {
		return info, nil
	}

	// First part is service name
	info.ServiceName = parts[0]

	// Check for version pattern
	if len(parts) > 1 && isVersionPattern(parts[1]) {
		info.ServiceVersion = strings.TrimPrefix(parts[1], "v")
		if len(parts) > 2 {
			info.ServicePath = "/" + strings.Join(parts[2:], "/")
		} else {
			info.ServicePath = "/"
		}
	} else if len(parts) > 1 {
		info.ServicePath = "/" + strings.Join(parts[1:], "/")
	}

	// Default version
	if info.ServiceVersion == "" {
		info.ServiceVersion = "1"
	}

	return info, nil
}

// ServiceURLInfo holds parsed service URL information
type ServiceURLInfo struct {
	OriginalPath   string
	ServiceName    string
	ServiceVersion string
	ServicePath    string
	Query          string
}

func isVersionPattern(s string) bool {
	if len(s) < 2 || s[0] != 'v' {
		return false
	}
	for _, c := range s[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// NormalizePath normalizes a URL path
func NormalizePath(path string) string {
	// Ensure leading slash
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// Remove double slashes first
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}

	// Remove trailing slash (except for root)
	if len(path) > 1 && strings.HasSuffix(path, "/") {
		path = strings.TrimSuffix(path, "/")
	}

	return path
}

// JoinPaths joins path segments
func JoinPaths(paths ...string) string {
	var result strings.Builder
	for _, path := range paths {
		if path == "" {
			continue
		}
		path = strings.TrimSuffix(path, "/")
		if result.Len() > 0 && !strings.HasPrefix(path, "/") {
			result.WriteString("/")
		}
		result.WriteString(path)
	}
	return result.String()
}
