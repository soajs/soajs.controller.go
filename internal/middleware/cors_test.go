package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultCORSConfig(t *testing.T) {
	cfg := DefaultCORSConfig()

	if !cfg.Enabled {
		t.Error("Expected CORS to be enabled by default")
	}

	if len(cfg.AllowedOrigins) == 0 {
		t.Error("Expected allowed origins to be set")
	}

	if len(cfg.AllowedMethods) == 0 {
		t.Error("Expected allowed methods to be set")
	}

	if len(cfg.AllowedHeaders) == 0 {
		t.Error("Expected allowed headers to be set")
	}

	if cfg.MaxAge <= 0 {
		t.Error("Expected MaxAge to be positive")
	}
}

func TestCORS_Preflight(t *testing.T) {
	handler := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	// Check CORS headers
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "http://example.com" {
		t.Errorf("Expected origin 'http://example.com', got '%s'", origin)
	}

	if methods := rr.Header().Get("Access-Control-Allow-Methods"); methods == "" {
		t.Error("Expected Access-Control-Allow-Methods to be set")
	}

	if headers := rr.Header().Get("Access-Control-Allow-Headers"); headers == "" {
		t.Error("Expected Access-Control-Allow-Headers to be set")
	}

	if maxAge := rr.Header().Get("Access-Control-Max-Age"); maxAge == "" {
		t.Error("Expected Access-Control-Max-Age to be set")
	}
}

func TestCORS_RegularRequest(t *testing.T) {
	handler := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Check CORS headers
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "http://example.com" {
		t.Errorf("Expected origin 'http://example.com', got '%s'", origin)
	}

	if credentials := rr.Header().Get("Access-Control-Allow-Credentials"); credentials != "true" {
		t.Errorf("Expected credentials 'true', got '%s'", credentials)
	}
}

func TestCORS_NoOrigin(t *testing.T) {
	handler := CORS()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// No Origin header set
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// Should default to "*"
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "*" {
		t.Errorf("Expected origin '*', got '%s'", origin)
	}
}

func TestCORSWithConfig_Disabled(t *testing.T) {
	cfg := CORSConfig{
		Enabled: false,
	}

	handler := CORSWithConfig(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://example.com")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// Should pass through without setting CORS headers
	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	// CORS headers should not be set
	if origin := rr.Header().Get("Access-Control-Allow-Origin"); origin != "" {
		t.Errorf("Expected no origin header, got '%s'", origin)
	}
}

func TestCORSWithConfig_CustomConfig(t *testing.T) {
	cfg := CORSConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		ExposedHeaders:   []string{"X-Custom"},
		AllowCredentials: false,
		MaxAge:           3600,
	}

	handler := CORSWithConfig(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if credentials := rr.Header().Get("Access-Control-Allow-Credentials"); credentials != "false" {
		t.Errorf("Expected credentials 'false', got '%s'", credentials)
	}
}
