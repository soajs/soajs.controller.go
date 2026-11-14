package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

func corsRegistry(cors types.CORSConfig) *mockRegistryMT {
	return &mockRegistryMT{data: &types.RegistryData{ServiceConfig: &types.GlobalServiceConfig{CORS: cors}}}
}

func TestCORSPreflight(t *testing.T) {
	reg := corsRegistry(types.CORSConfig{Enabled: true, Origin: "https://app.example.com", Headers: "key,Content-Type", MaxAge: 600})
	called := false
	handler := CORS(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("OPTIONS", "/svc/x", nil))
	if called || rec.Code != 204 {
		t.Fatalf("a preflight answers 204 without going on: %d", rec.Code)
	}
	want := map[string]string{
		"Access-Control-Allow-Origin":      "https://app.example.com",
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods":     "GET,HEAD,PUT,PATCH,POST,DELETE",
		"Access-Control-Allow-Headers":     "key,Content-Type",
		"Access-Control-Max-Age":           "600",
		"Access-Control-Expose-Headers":    "key,Content-Type",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestCORSSimpleRequestAndDisabled(t *testing.T) {
	reg := corsRegistry(types.CORSConfig{Enabled: true})
	handler := CORS(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/svc/x", nil))
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" || rec.Header().Get("Access-Control-Allow-Methods") != "" {
		t.Errorf("simple request headers: %v", rec.Header())
	}

	off := corsRegistry(types.CORSConfig{Enabled: false})
	rec = httptest.NewRecorder()
	CORS(off)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(rec, httptest.NewRequest("OPTIONS", "/svc/x", nil))
	if rec.Code != 200 || rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS disabled: no headers, the request goes on")
	}
}
