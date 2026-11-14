package lib

import (
	"net/http/httptest"
	"testing"
)

// The client IP is read as the Node.js gateway reads it.
func TestGetClientIP(t *testing.T) {
	tests := []struct {
		realIP, xff, remote, want string
	}{
		{"10.0.0.1", "10.0.0.2, 10.0.0.3", "10.0.0.4:5000", "10.0.0.1"},
		{"", "10.0.0.2, 10.0.0.3", "10.0.0.4:5000", "10.0.0.2"},
		{"", " 10.0.0.2,10.0.0.3", "10.0.0.4:5000", " 10.0.0.2"},
		{"", "", "10.0.0.4:5000", "10.0.0.4"},
		{"::ffff:10.0.0.5", "", "10.0.0.4:5000", "10.0.0.5"},
		{"", "", "[::ffff:10.0.0.6]:5000", "10.0.0.6"},
	}
	for _, tt := range tests {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = tt.remote
		if tt.realIP != "" {
			r.Header.Set("X-Real-IP", tt.realIP)
		}
		if tt.xff != "" {
			r.Header.Set("X-Forwarded-For", tt.xff)
		}
		if got := GetClientIP(r); got != tt.want {
			t.Errorf("GetClientIP(%+v) = %q, want %q", tt, got, tt.want)
		}
	}
}
