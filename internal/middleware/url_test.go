package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// parseURL follows the Node.js lib/parseURL.js and extractBuildParameters.
func TestParseURL(t *testing.T) {
	tests := []struct {
		uri, service, version, path string
		requested                   bool
	}{
		{"/orders/v1/list", "orders", "1", "/list", true},
		{"/orders/v12/a/b?x=1", "orders", "12", "/a/b", true},
		{"/orders/list", "orders", "", "/list", false},
		{"/orders/v1", "orders", "", "/v1", false},
		{"/orders/v1/", "orders", "1", "/", true},
		{"/orders/x/v1/y", "orders", "", "/x/v1/y", false},
		{"/orders:2/list", "orders", "2", "/list", true},
		{"/orders/list/?x=1", "orders", "", "/list", false},
		{"/orders/list/", "orders", "", "/list/", false},
		{"/soajs/acl", "soajs", "", "/acl", false},
		{"/", "", "", "", false},
	}
	for _, tt := range tests {
		info := parseURL(tt.uri)
		if info.ServiceName != tt.service || info.ServiceVersion != tt.version || info.ServicePath != tt.path || info.VersionRequested != tt.requested {
			t.Errorf("%s: got %q %q %q %v, want %q %q %q %v", tt.uri, info.ServiceName, info.ServiceVersion, info.ServicePath, info.VersionRequested,
				tt.service, tt.version, tt.path, tt.requested)
		}
	}
}

func TestURLMiddleware(t *testing.T) {
	reg := &mockRegistryMT{data: &types.RegistryData{Services: map[string]*types.ServiceConfig{"orders": {}}}}
	run := func(uri string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		Context(zap.NewNop(), "dev")(URL(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("key") != "" {
				w.Header().Set("X-Key", r.Header.Get("key"))
			}
			w.WriteHeader(200)
		}))).ServeHTTP(rec, httptest.NewRequest("GET", uri, nil))
		return rec
	}
	if rec := run("/"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"codes":[136]`) {
		t.Errorf("root: %d %s", rec.Code, rec.Body.String())
	}
	if rec := run("/unknown/v1/x"); rec.Code != 404 || !strings.Contains(rec.Body.String(), `"codes":[130]`) {
		t.Errorf("unknown service: %d %s", rec.Code, rec.Body.String())
	}
	if rec := run("/soajs/anything"); rec.Code != 200 {
		t.Errorf("/soajs goes on to the gateway routes: %d", rec.Code)
	}
	if rec := run("/orders/v1/x?key=abc"); rec.Code != 200 || rec.Header().Get("X-Key") != "abc" {
		t.Errorf("a query key becomes the key header: %v", rec.Header())
	}
}
