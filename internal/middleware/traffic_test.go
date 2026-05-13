package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

func TestTrafficMiddleware_AllowsNormalRequests(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	contextMW := Context(logger, "test")
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

func TestTrafficMiddleware_ThrottlingStore(t *testing.T) {
	// Reset the store for clean test
	throttlingStore.mu.Lock()
	throttlingStore.entries = make(map[string]*ThrottlingEntry)
	throttlingStore.mu.Unlock()

	// Test throttling check
	strategy := &ThrottlingStrategy{
		Status:  1,
		Type:    1,
		Window:  60000,
		Limit:   2,
		Retries: 0,
		Delay:   0,
	}

	// First request should pass
	allowed1, headers1 := checkThrottling("test-key-1", strategy, 0)
	if !allowed1 {
		t.Error("First request should be allowed")
	}
	if headers1["X-RateLimit-Remaining"] != "1" {
		t.Errorf("Remaining = %s, want 1", headers1["X-RateLimit-Remaining"])
	}

	// Second request should pass
	allowed2, headers2 := checkThrottling("test-key-1", strategy, 0)
	if !allowed2 {
		t.Error("Second request should be allowed")
	}
	if headers2["X-RateLimit-Remaining"] != "0" {
		t.Errorf("Remaining = %s, want 0", headers2["X-RateLimit-Remaining"])
	}

	// Third request should be rate limited (no retries)
	allowed3, _ := checkThrottling("test-key-1", strategy, 0)
	if allowed3 {
		t.Error("Third request should be rate limited")
	}

	// Different key should be allowed
	allowed4, _ := checkThrottling("test-key-2", strategy, 0)
	if !allowed4 {
		t.Error("Different key should be allowed")
	}

	// Cleanup
	throttlingStore.mu.Lock()
	throttlingStore.entries = make(map[string]*ThrottlingEntry)
	throttlingStore.mu.Unlock()
}

func TestBuildTrafficKey(t *testing.T) {
	// Type 0 = tenant only
	key0 := buildTrafficKey("tenant123", "192.168.1.1", 0)
	if key0 != "tenant:tenant123" {
		t.Errorf("Key type 0 = %s, want tenant:tenant123", key0)
	}

	// Type 1 = tenant + IP
	key1 := buildTrafficKey("tenant123", "192.168.1.1", 1)
	if key1 != "tenant:tenant123:ip:192.168.1.1" {
		t.Errorf("Key type 1 = %s, want tenant:tenant123:ip:192.168.1.1", key1)
	}
}

func TestSetRateLimit(t *testing.T) {
	// SetRateLimit is kept for compatibility but does nothing in new implementation
	SetRateLimit("custom-key", 50, 100)
	// No error = success
}
