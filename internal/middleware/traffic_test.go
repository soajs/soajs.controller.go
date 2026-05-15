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

func TestMemoryThrottlingModel(t *testing.T) {
	model := NewMemoryThrottlingModel()

	// Test throttling check with memory model
	strategy := &ThrottlingStrategy{
		Status:  1,
		Type:    1,
		Window:  60000,
		Limit:   2,
		Retries: 0,
		Delay:   0,
	}

	key := TrafficKey{L1: "tenant123", L2: "192.168.1.1"}

	// First request should pass
	entry1, _ := model.GetOrCreateEntry(key, strategy.Window)
	if entry1.Count != 1 {
		t.Errorf("First request count = %d, want 1", entry1.Count)
	}

	// Second request should pass
	entry2, _ := model.GetOrCreateEntry(key, strategy.Window)
	if entry2.Count != 2 {
		t.Errorf("Second request count = %d, want 2", entry2.Count)
	}

	// Third request should exceed limit
	entry3, _ := model.GetOrCreateEntry(key, strategy.Window)
	if entry3.Count != 3 {
		t.Errorf("Third request count = %d, want 3", entry3.Count)
	}
	if entry3.Count <= strategy.Limit {
		t.Error("Third request should exceed limit")
	}

	// Different key should start fresh
	key2 := TrafficKey{L1: "tenant456", L2: "192.168.1.2"}
	entry4, _ := model.GetOrCreateEntry(key2, strategy.Window)
	if entry4.Count != 1 {
		t.Errorf("Different key count = %d, want 1", entry4.Count)
	}
}

func TestBuildTrafficKeyStruct(t *testing.T) {
	// Type 0 = tenant only
	key0 := buildTrafficKeyStruct("tenant123", "192.168.1.1", 0)
	if key0.L1 != "tenant123" {
		t.Errorf("Key L1 = %s, want tenant123", key0.L1)
	}
	if key0.L2 != "_ALL_" {
		t.Errorf("Key L2 = %s, want _ALL_", key0.L2)
	}

	// Type 1 = tenant + IP
	key1 := buildTrafficKeyStruct("tenant123", "192.168.1.1", 1)
	if key1.L1 != "tenant123" {
		t.Errorf("Key L1 = %s, want tenant123", key1.L1)
	}
	if key1.L2 != "192.168.1.1" {
		t.Errorf("Key L2 = %s, want 192.168.1.1", key1.L2)
	}
}

func TestBuildTrafficKeyString(t *testing.T) {
	// Tenant only
	key1 := TrafficKey{L1: "tenant123", L2: "_ALL_"}
	str1 := BuildTrafficKeyString(key1)
	if str1 != "tenant:tenant123" {
		t.Errorf("Key string = %s, want tenant:tenant123", str1)
	}

	// Tenant + IP
	key2 := TrafficKey{L1: "tenant123", L2: "192.168.1.1"}
	str2 := BuildTrafficKeyString(key2)
	if str2 != "tenant:tenant123:ip:192.168.1.1" {
		t.Errorf("Key string = %s, want tenant:tenant123:ip:192.168.1.1", str2)
	}
}

func TestBuildTrafficKeyFromString(t *testing.T) {
	// Tenant only
	key1 := BuildTrafficKeyFromString("tenant:tenant123")
	if key1.L1 != "tenant123" {
		t.Errorf("Key L1 = %s, want tenant123", key1.L1)
	}
	if key1.L2 != "_ALL_" {
		t.Errorf("Key L2 = %s, want _ALL_", key1.L2)
	}

	// Tenant + IP
	key2 := BuildTrafficKeyFromString("tenant:tenant123:ip:192.168.1.1")
	if key2.L1 != "tenant123" {
		t.Errorf("Key L1 = %s, want tenant123", key2.L1)
	}
	if key2.L2 != "192.168.1.1" {
		t.Errorf("Key L2 = %s, want 192.168.1.1", key2.L2)
	}
}

func TestSetRateLimit(t *testing.T) {
	// SetRateLimit is kept for compatibility but does nothing in new implementation
	SetRateLimit("custom-key", 50, 100)
	// No error = success
}

func TestGetCurrentModel(t *testing.T) {
	// Default model should be memory
	model := GetCurrentModel()
	if model != "memory" {
		t.Errorf("Default model = %s, want memory", model)
	}
}

func TestCheckThrottlingWithMemoryModel(t *testing.T) {
	// Ensure memory model is being used
	manager.mu.Lock()
	manager.memoryModel = NewMemoryThrottlingModel()
	manager.currentModel = "memory"
	manager.mu.Unlock()

	strategy := &ThrottlingStrategy{
		Status:  1,
		Type:    1,
		Window:  60000,
		Limit:   2,
		Retries: 0,
		Delay:   0,
	}

	key := TrafficKey{L1: "test-tenant", L2: "10.0.0.1"}

	// First request should pass
	allowed1, headers1 := checkThrottlingWithModel(nil, key, strategy, "testStrategy", 0)
	if !allowed1 {
		t.Error("First request should be allowed")
	}
	if headers1["X-RateLimit-Remaining"] != "1" {
		t.Errorf("Remaining = %s, want 1", headers1["X-RateLimit-Remaining"])
	}

	// Second request should pass
	allowed2, headers2 := checkThrottlingWithModel(nil, key, strategy, "testStrategy", 0)
	if !allowed2 {
		t.Error("Second request should be allowed")
	}
	if headers2["X-RateLimit-Remaining"] != "0" {
		t.Errorf("Remaining = %s, want 0", headers2["X-RateLimit-Remaining"])
	}

	// Third request should be rate limited
	allowed3, _ := checkThrottlingWithModel(nil, key, strategy, "testStrategy", 0)
	if allowed3 {
		t.Error("Third request should be rate limited")
	}

	// Different key should be allowed
	key2 := TrafficKey{L1: "test-tenant-2", L2: "10.0.0.2"}
	allowed4, _ := checkThrottlingWithModel(nil, key2, strategy, "testStrategy", 0)
	if !allowed4 {
		t.Error("Different key should be allowed")
	}

	// Reset the model for other tests
	manager.mu.Lock()
	manager.memoryModel = NewMemoryThrottlingModel()
	manager.mu.Unlock()
}

func TestBuildRateLimitHeaders(t *testing.T) {
	headers := buildRateLimitHeaders(5000, 100, 50)

	if headers["Retry-After"] != "5" {
		t.Errorf("Retry-After = %s, want 5", headers["Retry-After"])
	}
	if headers["X-RateLimit-Limit"] != "100" {
		t.Errorf("X-RateLimit-Limit = %s, want 100", headers["X-RateLimit-Limit"])
	}
	if headers["X-RateLimit-Remaining"] != "50" {
		t.Errorf("X-RateLimit-Remaining = %s, want 50", headers["X-RateLimit-Remaining"])
	}

	// Test with negative remaining
	headers2 := buildRateLimitHeaders(1000, 10, -5)
	if headers2["X-RateLimit-Remaining"] != "0" {
		t.Errorf("X-RateLimit-Remaining = %s, want 0 (negative clamped)", headers2["X-RateLimit-Remaining"])
	}

	// Test with very small remaining time
	headers3 := buildRateLimitHeaders(500, 10, 5)
	if headers3["Retry-After"] != "1" {
		t.Errorf("Retry-After = %s, want 1 (minimum)", headers3["Retry-After"])
	}
}
