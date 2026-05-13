package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/config"
	"go.uber.org/zap"
)

func TestParseURL(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		wantService    string
		wantVersion    string
		wantPath       string
	}{
		{
			name:        "service only",
			path:        "/urac",
			wantService: "urac",
			wantVersion: "1",
			wantPath:    "",
		},
		{
			name:        "service with path",
			path:        "/urac/user/get",
			wantService: "urac",
			wantVersion: "1",
			wantPath:    "/user/get",
		},
		{
			name:        "service with version",
			path:        "/urac/v2/user/get",
			wantService: "urac",
			wantVersion: "2",
			wantPath:    "/user/get",
		},
		{
			name:        "service with v1",
			path:        "/api/v1/endpoint",
			wantService: "api",
			wantVersion: "1",
			wantPath:    "/endpoint",
		},
		{
			name:        "empty path",
			path:        "/",
			wantService: "",
			wantVersion: "1",
			wantPath:    "",
		},
		{
			name:        "deep path no version",
			path:        "/dashboard/admin/settings/general",
			wantService: "dashboard",
			wantVersion: "1",
			wantPath:    "/admin/settings/general",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := parseURL(tt.path)

			if info.ServiceName != tt.wantService {
				t.Errorf("ServiceName = %q, want %q", info.ServiceName, tt.wantService)
			}
			if info.ServiceVersion != tt.wantVersion {
				t.Errorf("ServiceVersion = %q, want %q", info.ServiceVersion, tt.wantVersion)
			}
			if info.ServicePath != tt.wantPath {
				t.Errorf("ServicePath = %q, want %q", info.ServicePath, tt.wantPath)
			}
		})
	}
}

func TestIsVersionPattern(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"v1", true},
		{"v2", true},
		{"v10", true},
		{"v123", true},
		{"V1", false},
		{"1", false},
		{"v", false},
		{"va", false},
		{"v1a", false},
		{"version1", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := isVersionPattern(tt.input); got != tt.want {
				t.Errorf("isVersionPattern(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestURLMiddleware(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Create context middleware first
	contextMW := Context(logger, "test")
	urlMW := URL()

	handler := contextMW(urlMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		soajsCtx := GetSOAJSContext(ctx)

		if soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil {
			t.Error("SOAJS context not properly initialized")
			return
		}

		urlInfo := soajsCtx.Controller.ServiceParams.URLInfo
		if urlInfo == nil {
			t.Error("URLInfo not set")
			return
		}

		if urlInfo.ServiceName != "testservice" {
			t.Errorf("ServiceName = %q, want %q", urlInfo.ServiceName, "testservice")
		}

		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/testservice/v1/action", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

// Ensure config is imported
var _ = config.HeaderSOAJSKey
