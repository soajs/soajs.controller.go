package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestTrafficMiddleware_AllowsNormalRequests(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	contextMW := Context(logger)
	trafficMW := Traffic(nil)

	handler := contextMW(trafficMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestTrafficMiddleware_RateLimiting(t *testing.T) {
	// Reset the store for clean test
	trafficStore.mu.Lock()
	trafficStore.limiters = make(map[string]*rateLimiterEntry)
	trafficStore.config = TrafficConfig{
		Enabled:      true,
		DefaultLimit: 1, // Very low for testing
		DefaultBurst: 1,
	}
	trafficStore.mu.Unlock()

	logger, _ := zap.NewDevelopment()

	contextMW := Context(logger)
	trafficMW := Traffic(nil)

	handler := contextMW(trafficMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	// First request should succeed
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.RemoteAddr = "192.168.1.100:12345"
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("First request: Status = %d, want %d", rec1.Code, http.StatusOK)
	}

	// Second request from same IP should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.RemoteAddr = "192.168.1.100:12345"
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("Second request: Status = %d, want %d", rec2.Code, http.StatusTooManyRequests)
	}

	// Request from different IP should succeed
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.RemoteAddr = "192.168.1.200:12345"
	rec3 := httptest.NewRecorder()
	handler.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusOK {
		t.Errorf("Different IP request: Status = %d, want %d", rec3.Code, http.StatusOK)
	}

	// Reset config to defaults
	trafficStore.mu.Lock()
	trafficStore.config = DefaultTrafficConfig()
	trafficStore.limiters = make(map[string]*rateLimiterEntry)
	trafficStore.mu.Unlock()
}

func TestSetRateLimit(t *testing.T) {
	SetRateLimit("custom-key", 50, 100)

	trafficStore.mu.RLock()
	entry, exists := trafficStore.limiters["custom-key"]
	trafficStore.mu.RUnlock()

	if !exists {
		t.Error("Custom rate limit not set")
	}

	if entry == nil {
		t.Error("Rate limiter entry is nil")
	}
}
