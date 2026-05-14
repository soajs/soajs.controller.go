package security

import (
	"encoding/base64"
	"testing"
)

func TestGetAuth(t *testing.T) {
	tests := []struct {
		name     string
		auth     string
		expected string
	}{
		{
			name:     "valid auth header",
			auth:     "Basic " + base64.StdEncoding.EncodeToString([]byte("soajs:C01abc123def456ghij789klmnop0123")),
			expected: "C01abc123def456ghij789klmnop0123",
		},
		{
			name:     "valid auth with whitespace",
			auth:     "  Basic " + base64.StdEncoding.EncodeToString([]byte("soajs:C02xyz789")),
			expected: "C02xyz789",
		},
		{
			name:     "missing Basic prefix",
			auth:     base64.StdEncoding.EncodeToString([]byte("soajs:session123")),
			expected: "",
		},
		{
			name:     "missing soajs prefix",
			auth:     "Basic " + base64.StdEncoding.EncodeToString([]byte("other:session123")),
			expected: "",
		},
		{
			name:     "invalid base64",
			auth:     "Basic not-valid-base64!!!",
			expected: "",
		},
		{
			name:     "empty string",
			auth:     "",
			expected: "",
		},
		{
			name:     "only Basic",
			auth:     "Basic ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetAuth(tt.auth)
			if result != tt.expected {
				t.Errorf("GetAuth(%q) = %q, want %q", tt.auth, result, tt.expected)
			}
		})
	}
}

func TestUmask(t *testing.T) {
	tests := []struct {
		name     string
		styleID  string
		expected string
	}{
		{
			name:     "C01 cypher - valid 32 char",
			styleID:  "C01" + "abcdefghijklmnop0123456789ABCDEF",
			expected: "abclefgpijkdmnoh0123456789ABCDEF", // C01 decypher result
		},
		{
			name:     "C02 cypher - valid 32 char",
			styleID:  "C02" + "abcdefghijklmnop0123456789ABCDEF",
			expected: "abcdmfghipklenoj0123456789ABCDEF", // C02 decypher result
		},
		{
			name:     "C03 cypher - valid 32 char",
			styleID:  "C03" + "abcdefghijklmnop0123456789ABCDEF",
			expected: "abcdenghijkpmfol0123456789ABCDEF", // C03 decypher result
		},
		{
			name:     "unknown cypher - returns without prefix",
			styleID:  "XXXsession123456",
			expected: "session123456",
		},
		{
			name:     "short input - less than 3 chars",
			styleID:  "AB",
			expected: "",
		},
		{
			name:     "empty string",
			styleID:  "",
			expected: "",
		},
		{
			name:     "C01 with wrong length - returns empty",
			styleID:  "C01short",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Umask(tt.styleID)
			if result != tt.expected {
				t.Errorf("Umask(%q) = %q, want %q", tt.styleID, result, tt.expected)
			}
		})
	}
}

func TestSign(t *testing.T) {
	tests := []struct {
		name   string
		val    string
		secret string
	}{
		{
			name:   "simple value",
			val:    "session123",
			secret: "mysecret",
		},
		{
			name:   "empty value",
			val:    "",
			secret: "mysecret",
		},
		{
			name:   "empty secret",
			val:    "session123",
			secret: "",
		},
		{
			name:   "unicode value",
			val:    "session-été-日本",
			secret: "secret123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Sign(tt.val, tt.secret)

			// Result should be val.signature
			if len(result) <= len(tt.val)+1 {
				t.Errorf("Sign result too short: %q", result)
			}

			// Should start with val.
			expected := tt.val + "."
			if result[:len(expected)] != expected {
				t.Errorf("Sign(%q, %q) should start with %q, got %q", tt.val, tt.secret, expected, result)
			}

			// Signature should not end with =
			if len(result) > 0 && result[len(result)-1] == '=' {
				t.Errorf("Sign result should not end with '=': %q", result)
			}

			// Same inputs should produce same output
			result2 := Sign(tt.val, tt.secret)
			if result != result2 {
				t.Errorf("Sign not deterministic: %q != %q", result, result2)
			}
		})
	}
}

func TestSign_DifferentSecrets(t *testing.T) {
	val := "session123"
	sig1 := Sign(val, "secret1")
	sig2 := Sign(val, "secret2")

	if sig1 == sig2 {
		t.Error("Different secrets should produce different signatures")
	}
}

func TestSetCookie(t *testing.T) {
	// Create a valid auth header with a 32-char session ID
	sessionID := "abcdefghijklmnop0123456789ABCDEF"
	maskedID := "C01" + sessionID
	validAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("soajs:"+maskedID))

	tests := []struct {
		name       string
		auth       string
		secret     string
		cookieName string
		wantEmpty  bool
	}{
		{
			name:       "valid auth",
			auth:       validAuth,
			secret:     "cookie-secret",
			cookieName: "soajsID",
			wantEmpty:  false,
		},
		{
			name:       "invalid auth",
			auth:       "invalid",
			secret:     "cookie-secret",
			cookieName: "soajsID",
			wantEmpty:  true,
		},
		{
			name:       "empty auth",
			auth:       "",
			secret:     "cookie-secret",
			cookieName: "soajsID",
			wantEmpty:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SetCookie(tt.auth, tt.secret, tt.cookieName)

			if tt.wantEmpty {
				if result != "" {
					t.Errorf("SetCookie should return empty, got %q", result)
				}
				return
			}

			// Should start with cookieName=s:
			prefix := tt.cookieName + "=s:"
			if len(result) < len(prefix) || result[:len(prefix)] != prefix {
				t.Errorf("SetCookie should start with %q, got %q", prefix, result)
			}

			// Should contain signature (dot followed by chars)
			if !contains(result, ".") {
				t.Errorf("SetCookie should contain signature dot: %q", result)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestCyphers_Reversible(t *testing.T) {
	// Test that cypher and decypher are inverses
	sessionID := "abcdefghijklmnop0123456789ABCDEF"

	for code, cypher := range cyphers {
		t.Run(code, func(t *testing.T) {
			// Cypher then decypher should give original
			cyphered := cypher.Cypher(sessionID)
			decyphered := cypher.Decypher(cyphered)

			// For these particular cyphers, cypher == decypher (self-inverse)
			if decyphered != sessionID {
				t.Errorf("%s: Cypher then Decypher failed: got %q, want %q", code, decyphered, sessionID)
			}
		})
	}
}

func TestCyphers_WrongLength(t *testing.T) {
	for code, cypher := range cyphers {
		t.Run(code+"_short", func(t *testing.T) {
			result := cypher.Cypher("short")
			if result != "" {
				t.Errorf("%s Cypher should return empty for short input, got %q", code, result)
			}
		})

		t.Run(code+"_long", func(t *testing.T) {
			result := cypher.Cypher("this-is-way-too-long-for-a-session-id-12345")
			if result != "" {
				t.Errorf("%s Cypher should return empty for long input, got %q", code, result)
			}
		})
	}
}
