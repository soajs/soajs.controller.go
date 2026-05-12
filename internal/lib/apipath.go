package lib

import (
	"regexp"
	"strings"
)

// APIPathToRegex converts an API path pattern to a regular expression
// Supports :param and * wildcards
func APIPathToRegex(pattern string) (*regexp.Regexp, error) {
	// Escape regex special characters except : and *
	escaped := regexp.QuoteMeta(pattern)

	// Replace escaped \: with parameter pattern
	escaped = strings.ReplaceAll(escaped, `\:`, ":")

	// Replace :param with named capture group
	paramRegex := regexp.MustCompile(`:(\w+)`)
	escaped = paramRegex.ReplaceAllString(escaped, `(?P<$1>[^/]+)`)

	// Replace * with wildcard
	escaped = strings.ReplaceAll(escaped, `\*`, `.*`)

	// Add anchors
	escaped = "^" + escaped + "$"

	return regexp.Compile(escaped)
}

// MatchPath checks if a path matches a pattern and extracts parameters
func MatchPath(pattern, path string) (bool, map[string]string) {
	regex, err := APIPathToRegex(pattern)
	if err != nil {
		return false, nil
	}

	matches := regex.FindStringSubmatch(path)
	if matches == nil {
		return false, nil
	}

	params := make(map[string]string)
	names := regex.SubexpNames()
	for i, name := range names {
		if i > 0 && name != "" && i < len(matches) {
			params[name] = matches[i]
		}
	}

	return true, params
}

// ExtractPathParams extracts parameters from a path based on a pattern
func ExtractPathParams(pattern, path string) map[string]string {
	_, params := MatchPath(pattern, path)
	return params
}

// NormalizeAPIPath normalizes an API path for comparison
func NormalizeAPIPath(path string) string {
	// Remove leading and trailing slashes
	path = strings.Trim(path, "/")

	// Add leading slash back
	if path != "" {
		path = "/" + path
	} else {
		path = "/"
	}

	return path
}
