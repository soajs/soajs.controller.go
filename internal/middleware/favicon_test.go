package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFavicon_FaviconRequest(t *testing.T) {
	nextCalled := false
	handler := Favicon()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/favicon.ico", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("Expected status %d for favicon, got %d", http.StatusNoContent, rr.Code)
	}

	if nextCalled {
		t.Error("Next handler should not be called for favicon requests")
	}
}

func TestFavicon_OtherRequest(t *testing.T) {
	nextCalled := false
	handler := Favicon()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rr.Code)
	}

	if !nextCalled {
		t.Error("Next handler should be called for non-favicon requests")
	}
}

func TestFavicon_SimilarPath(t *testing.T) {
	nextCalled := false
	handler := Favicon()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	// Path that contains 'favicon' but isn't exactly '/favicon.ico'
	req := httptest.NewRequest(http.MethodGet, "/api/favicon.ico/test", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("Next handler should be called for paths that aren't exactly /favicon.ico")
	}
}
