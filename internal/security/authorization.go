package security

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strings"
)

// Cyphers for session ID masking/unmasking
var cyphers = map[string]struct {
	Cypher   func(string) string
	Decypher func(string) string
}{
	"C01": {
		Cypher: func(sid string) string {
			if len(sid) != 32 {
				return ""
			}
			return sid[0:3] + string(sid[11]) + sid[4:7] + string(sid[15]) + sid[8:11] + string(sid[3]) + sid[12:15] + string(sid[7]) + sid[16:]
		},
		Decypher: func(sid string) string {
			if len(sid) != 32 {
				return ""
			}
			return sid[0:3] + string(sid[11]) + sid[4:7] + string(sid[15]) + sid[8:11] + string(sid[3]) + sid[12:15] + string(sid[7]) + sid[16:]
		},
	},
	"C02": {
		Cypher: func(sid string) string {
			if len(sid) != 32 {
				return ""
			}
			return sid[0:4] + string(sid[12]) + sid[5:9] + string(sid[15]) + sid[10:12] + string(sid[4]) + sid[13:15] + string(sid[9]) + sid[16:]
		},
		Decypher: func(sid string) string {
			if len(sid) != 32 {
				return ""
			}
			return sid[0:4] + string(sid[12]) + sid[5:9] + string(sid[15]) + sid[10:12] + string(sid[4]) + sid[13:15] + string(sid[9]) + sid[16:]
		},
	},
	"C03": {
		Cypher: func(sid string) string {
			if len(sid) != 32 {
				return ""
			}
			return sid[0:5] + string(sid[13]) + sid[6:11] + string(sid[15]) + sid[12:13] + string(sid[5]) + sid[14:15] + string(sid[11]) + sid[16:]
		},
		Decypher: func(sid string) string {
			if len(sid) != 32 {
				return ""
			}
			return sid[0:5] + string(sid[13]) + sid[6:11] + string(sid[15]) + sid[12:13] + string(sid[5]) + sid[14:15] + string(sid[11]) + sid[16:]
		},
	},
}

// GetAuth extracts the masked session ID from soajsauth header/query
func GetAuth(auth string) string {
	auth = strings.TrimSpace(auth)
	parts := strings.Split(auth, "Basic ")
	if len(parts) < 2 {
		return ""
	}

	base64Part := parts[1]
	decoded, err := base64.StdEncoding.DecodeString(base64Part)
	if err != nil {
		return ""
	}

	ascii := string(decoded)
	if !strings.HasPrefix(ascii, "soajs:") {
		return ""
	}

	return strings.TrimPrefix(ascii, "soajs:")
}

// Umask decyphers the session ID
func Umask(styleID string) string {
	if len(styleID) < 3 {
		return ""
	}

	styleCode := styleID[0:3]
	cypher, ok := cyphers[styleCode]
	if !ok {
		return styleID[3:]
	}

	return cypher.Decypher(styleID[3:])
}

// Sign creates HMAC-SHA256 signature
func Sign(val, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(val))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	signature = strings.TrimRight(signature, "=")
	return val + "." + signature
}

// SetCookie converts soajsauth to a signed session cookie
func SetCookie(auth, secret, cookieName string) string {
	masked := GetAuth(auth)
	if masked == "" {
		return ""
	}

	sessionID := Umask(masked)
	if sessionID == "" {
		return ""
	}

	signed := "s:" + Sign(sessionID, secret)
	return cookieName + "=" + signed
}
