package lib

import (
	"errors"
	"net"
	"strconv"
	"strings"
)

// ErrInvalidURL is a request URL that WHATWG URL parsing refuses.
var ErrInvalidURL = errors.New("Invalid URL")

// RequestURL parses a request target as Node.js new URL(target, "http://localhost") does
// (WHATWG URL, special scheme): it returns the pathname and the search ("?..." or "").
//   - leading and trailing C0 controls and spaces are stripped, tabs and newlines removed;
//   - "\" is a "/"; a target that starts with two of them names a host, which must be valid, and
//     the path follows it;
//   - "." and ".." segments, also as %2e, are resolved;
//   - the path is percent-encoded with the path set (C0 controls, space, " # < > ? ^ ` { } and
//     above ~), the query with the special-query set (C0 controls, space, " # < > ' and above ~);
//     an existing %xx is kept as is, %2F among them;
//   - the fragment is dropped.
func RequestURL(target string) (pathname, search string, err error) {
	s := strings.TrimFunc(target, func(r rune) bool { return r <= 0x20 })
	s = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, s)
	if i := strings.IndexByte(s, '#'); i != -1 {
		s = s[:i]
	}
	query := ""
	hasQuery := false
	if i := strings.IndexByte(s, '?'); i != -1 {
		query, hasQuery = s[i+1:], true
		s = s[:i]
	}

	// an absolute target: scheme://authority/path
	if scheme, rest, ok := splitScheme(s); ok {
		if !specialScheme(scheme) {
			return encode(rest, inC0Set), searchOf(query, hasQuery), nil
		}
		rest = strings.TrimLeft(rest, "/\\")
		authority, path := splitAuthority(rest)
		if err := validAuthority(authority); err != nil {
			return "", "", err
		}
		return resolvePath(path), searchOf(query, hasQuery), nil
	}
	if len(s) >= 2 && isSlash(s[0]) && isSlash(s[1]) {
		// a special scheme skips every slash before the host
		authority, path := splitAuthority(strings.TrimLeft(s, "/\\"))
		if err := validAuthority(authority); err != nil {
			return "", "", err
		}
		return resolvePath(path), searchOf(query, hasQuery), nil
	}
	if len(s) > 0 && isSlash(s[0]) {
		return resolvePath(s), searchOf(query, hasQuery), nil
	}
	// relative to the base path "/"
	return resolvePath("/" + s), searchOf(query, hasQuery), nil
}

func isSlash(c byte) bool { return c == '/' || c == '\\' }

func splitScheme(s string) (string, string, bool) {
	i := strings.IndexByte(s, ':')
	if i <= 0 {
		return "", "", false
	}
	for j := 0; j < i; j++ {
		c := s[j]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || j > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.')) {
			return "", "", false
		}
	}
	return strings.ToLower(s[:i]), s[i+1:], true
}

func specialScheme(s string) bool {
	switch s {
	case "http", "https", "ws", "wss", "ftp":
		return true
	}
	return false
}

// splitAuthority splits "authority/path" at the first slash or backslash.
func splitAuthority(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if isSlash(s[i]) {
			return s[:i], s[i:]
		}
	}
	return s, ""
}

// validAuthority checks [userinfo@]host[:port] as the WHATWG host parser does.
func validAuthority(authority string) error {
	if i := strings.LastIndexByte(authority, '@'); i != -1 {
		authority = authority[i+1:]
	}
	host, port := authority, ""
	if strings.HasPrefix(host, "[") {
		end := strings.IndexByte(host, ']')
		if end == -1 {
			return ErrInvalidURL
		}
		ip := net.ParseIP(host[1:end])
		if ip == nil || !strings.Contains(host[1:end], ":") {
			return ErrInvalidURL
		}
		rest := host[end+1:]
		if rest != "" {
			if rest[0] != ':' {
				return ErrInvalidURL
			}
			port = rest[1:]
		}
		return validPort(port)
	}
	if i := strings.LastIndexByte(host, ':'); i != -1 {
		host, port = host[:i], host[i+1:]
	}
	if err := validPort(port); err != nil {
		return err
	}
	if host == "" {
		return ErrInvalidURL
	}
	decoded, ok := percentDecode(host)
	if !ok {
		return ErrInvalidURL
	}
	decoded = strings.ToLower(decoded)
	for _, r := range decoded {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(" #%/:<>?@[\\]^|", r) {
			return ErrInvalidURL
		}
	}
	if endsInNumber(decoded) && !validIPv4(decoded) {
		return ErrInvalidURL
	}
	return nil
}

func validPort(p string) error {
	if p == "" {
		return nil
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return ErrInvalidURL
		}
	}
	if n, err := strconv.Atoi(p); err != nil || n > 65535 {
		return ErrInvalidURL
	}
	return nil
}

// percentDecode decodes %xx; a % that does not start one is kept, and refused by the caller.
func percentDecode(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHex(s[i+1]) && isHex(s[i+2]) {
			v, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(v))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String(), true
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func ipv4Parts(host string) []string {
	parts := strings.Split(host, ".")
	if len(parts) > 1 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

// endsInNumber: the last label is decimal digits, or 0x and hex digits.
func endsInNumber(host string) bool {
	parts := ipv4Parts(host)
	last := parts[len(parts)-1]
	if last == "" {
		return false
	}
	if strings.Trim(last, "0123456789") == "" {
		return true
	}
	if strings.HasPrefix(last, "0x") {
		h := last[2:]
		return strings.Trim(h, "0123456789abcdef") == ""
	}
	return false
}

func validIPv4(host string) bool {
	parts := ipv4Parts(host)
	if len(parts) > 4 {
		return false
	}
	nums := make([]uint64, len(parts))
	for i, p := range parts {
		if p == "" {
			return false
		}
		base, digits := 10, p
		switch {
		case strings.HasPrefix(p, "0x"):
			base, digits = 16, p[2:]
			if digits == "" {
				digits = "0"
			}
		case len(p) > 1 && p[0] == '0':
			base, digits = 8, p[1:]
		}
		n, err := strconv.ParseUint(digits, base, 64)
		if err != nil {
			return false
		}
		nums[i] = n
	}
	for i := 0; i < len(nums)-1; i++ {
		if nums[i] > 255 {
			return false
		}
	}
	limit := uint64(1) << (8 * uint(5-len(nums)))
	return nums[len(nums)-1] < limit
}

// resolvePath resolves the segments of a path that starts with a slash, encoding each one.
func resolvePath(path string) string {
	if path == "" {
		return "/"
	}
	segments := splitSegments(path[1:])
	var out []string
	for i, seg := range segments {
		last := i == len(segments)-1
		lower := strings.ToLower(seg)
		switch lower {
		case ".", "%2e":
			if last {
				out = append(out, "")
			}
		case "..", ".%2e", "%2e.", "%2e%2e":
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			out = append(out, encode(seg, inPathSet))
		}
	}
	return "/" + strings.Join(out, "/")
}

func splitSegments(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if isSlash(s[i]) {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func searchOf(query string, has bool) string {
	if !has || query == "" {
		return ""
	}
	return "?" + encode(query, inQuerySet)
}

func inC0Set(c byte) bool    { return c < 0x20 || c > 0x7e }
func inPathSet(c byte) bool  { return inC0Set(c) || strings.IndexByte(" \"#<>?^`{}", c) >= 0 }
func inQuerySet(c byte) bool { return inC0Set(c) || strings.IndexByte(" \"#<>'", c) >= 0 }

// encode percent-encodes the bytes of s in the set, uppercase hex.
func encode(s string, in func(byte) bool) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if in(c) {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
