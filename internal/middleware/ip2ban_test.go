package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

func TestIP2Ban(t *testing.T) {
	reg := &mockRegistryMT{data: &types.RegistryData{CustomRegistry: map[string]interface{}{
		"gateway": map[string]interface{}{"value": map[string]interface{}{"traffic": map[string]interface{}{
			"ip2ban": []interface{}{"10.0.0.9", "10.1.0.0/16"},
		}}},
	}}}
	handler := IP2BanWithRegistry(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	tests := []struct {
		realIP string
		status int
	}{
		{"10.0.0.9", 403},
		{"10.0.0.8", 200},
		{"10.1.2.3", 200}, // exact match only, as in Node.js
	}
	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Real-IP", tt.realIP)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != tt.status {
			t.Errorf("%s: status %d, want %d", tt.realIP, rec.Code, tt.status)
		}
		if tt.status == 403 && rec.Body.String() != `{"status":403,"msg":"banned"}` {
			t.Errorf("body %s", rec.Body.String())
		}
	}
}
