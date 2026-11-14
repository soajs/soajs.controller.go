package lib

import (
	"regexp"
	"strings"
)

// PathToRegexp compiles an API route as path-to-regexp 8 does for the Node.js gateway (sensitive,
// matching the whole path with an optional trailing slash): ":name" matches one segment,
// "*name" the rest of the path, and "{...}" is optional.
func PathToRegexp(route string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(route); i++ {
		c := route[i]
		switch {
		case c == ':' || c == '*':
			j := i + 1
			for j < len(route) && isNameChar(route[j], j == i+1) {
				j++
			}
			if j == i+1 {
				b.WriteString(regexp.QuoteMeta(string(c)))
				continue
			}
			if c == ':' {
				b.WriteString(`([^/]+?)`)
			} else {
				b.WriteString(`(.+?)`)
			}
			i = j - 1
		case c == '{':
			b.WriteString("(?:")
		case c == '}':
			b.WriteString(")?")
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`(?:/)?$`)
	return regexp.Compile(b.String())
}

func isNameChar(c byte, first bool) bool {
	if c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
		return true
	}
	return !first && c >= '0' && c <= '9'
}
