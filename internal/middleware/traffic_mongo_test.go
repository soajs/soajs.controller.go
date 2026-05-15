package middleware

import (
	"testing"
	"time"
)

// TestMongoThrottlingModel_Interface tests that MongoThrottlingModel implements ThrottlingModel
func TestMongoThrottlingModel_Interface(t *testing.T) {
	// This is a compile-time check that MongoThrottlingModel implements ThrottlingModel
	var _ ThrottlingModel = (*MongoThrottlingModel)(nil)
}

// TestMemoryThrottlingModel_Interface tests that MemoryThrottlingModel implements ThrottlingModel
func TestMemoryThrottlingModel_Interface(t *testing.T) {
	// This is a compile-time check that MemoryThrottlingModel implements ThrottlingModel
	var _ ThrottlingModel = (*MemoryThrottlingModel)(nil)
}

// TestTrafficKey tests TrafficKey operations
func TestTrafficKey(t *testing.T) {
	tests := []struct {
		name     string
		key      TrafficKey
		expected string
	}{
		{
			name:     "tenant only",
			key:      TrafficKey{L1: "tenant1", L2: "_ALL_"},
			expected: "tenant:tenant1",
		},
		{
			name:     "tenant with IP",
			key:      TrafficKey{L1: "tenant1", L2: "192.168.1.1"},
			expected: "tenant:tenant1:ip:192.168.1.1",
		},
		{
			name:     "empty L2",
			key:      TrafficKey{L1: "tenant1", L2: ""},
			expected: "tenant:tenant1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildTrafficKeyString(tt.key)
			if result != tt.expected {
				t.Errorf("BuildTrafficKeyString() = %s, want %s", result, tt.expected)
			}
		})
	}
}

// TestMemoryThrottlingModel_Cleanup tests cleanup of expired entries
func TestMemoryThrottlingModel_Cleanup(t *testing.T) {
	model := NewMemoryThrottlingModel()

	// Create an old entry manually
	key := TrafficKey{L1: "old-tenant", L2: "_ALL_"}
	model.mu.Lock()
	model.entries[BuildTrafficKeyString(key)] = &ThrottlingModelEntry{
		L1:           key.L1,
		L2:           key.L2,
		FirstReqTime: time.Now().Add(-2 * time.Hour), // 2 hours ago
		Count:        5,
	}
	model.mu.Unlock()

	// Verify it exists
	entry, _ := model.GetThrottling(key)
	if entry == nil {
		t.Error("Entry should exist before cleanup")
	}

	// Run cleanup with 1 hour max age
	model.Cleanup(time.Hour)

	// Verify it's been removed
	entry, _ = model.GetThrottling(key)
	if entry != nil {
		t.Error("Old entry should be cleaned up")
	}
}

// TestMemoryThrottlingModel_ResetThrottling tests resetting throttling
func TestMemoryThrottlingModel_ResetThrottling(t *testing.T) {
	model := NewMemoryThrottlingModel()

	key := TrafficKey{L1: "test-tenant", L2: "192.168.1.1"}

	// Create entry with some requests
	model.GetOrCreateEntry(key, 60000)
	model.GetOrCreateEntry(key, 60000)
	model.GetOrCreateEntry(key, 60000)

	entry, _ := model.GetThrottling(key)
	if entry.Count != 3 {
		t.Errorf("Count before reset = %d, want 3", entry.Count)
	}

	// Reset
	err := model.ResetThrottling(key)
	if err != nil {
		t.Errorf("ResetThrottling error: %v", err)
	}

	entry, _ = model.GetThrottling(key)
	if entry.Count != 1 {
		t.Errorf("Count after reset = %d, want 1", entry.Count)
	}
}

// TestMemoryThrottlingModel_IncrementThrottling tests incrementing throttling
func TestMemoryThrottlingModel_IncrementThrottling(t *testing.T) {
	model := NewMemoryThrottlingModel()

	key := TrafficKey{L1: "inc-tenant", L2: "_ALL_"}

	// Initial increment should create entry
	err := model.IncrementThrottling(key)
	if err != nil {
		t.Errorf("IncrementThrottling error: %v", err)
	}

	entry, _ := model.GetThrottling(key)
	if entry.Count != 1 {
		t.Errorf("Count after first increment = %d, want 1", entry.Count)
	}

	// Second increment
	err = model.IncrementThrottling(key)
	if err != nil {
		t.Errorf("IncrementThrottling error: %v", err)
	}

	entry, _ = model.GetThrottling(key)
	if entry.Count != 2 {
		t.Errorf("Count after second increment = %d, want 2", entry.Count)
	}
}

// TestMemoryThrottlingModel_LogTooManyRequests tests error logging
func TestMemoryThrottlingModel_LogTooManyRequests(t *testing.T) {
	model := NewMemoryThrottlingModel()

	key := TrafficKey{L1: "error-tenant", L2: "10.0.0.1"}

	// Memory model doesn't persist error logs, but should not error
	err := model.LogTooManyRequests(key, "testStrategy")
	if err != nil {
		t.Errorf("LogTooManyRequests error: %v", err)
	}
}

// TestMemoryThrottlingModel_GetRemainingTime tests remaining time calculation
func TestMemoryThrottlingModel_GetRemainingTime(t *testing.T) {
	model := NewMemoryThrottlingModel()

	key := TrafficKey{L1: "time-tenant", L2: "_ALL_"}
	windowMs := 60000

	// No entry yet - should return 0
	remaining := model.GetRemainingTime(key, windowMs)
	if remaining != 0 {
		t.Errorf("Remaining time with no entry = %v, want 0", remaining)
	}

	// Create entry
	model.GetOrCreateEntry(key, windowMs)

	// Should have close to 60 seconds remaining
	remaining = model.GetRemainingTime(key, windowMs)
	if remaining < 59*time.Second || remaining > 60*time.Second {
		t.Errorf("Remaining time = %v, want ~60s", remaining)
	}
}

// TestMemoryThrottlingModel_WindowExpiry tests window expiry behavior
func TestMemoryThrottlingModel_WindowExpiry(t *testing.T) {
	model := NewMemoryThrottlingModel()

	key := TrafficKey{L1: "expiry-tenant", L2: "_ALL_"}
	windowMs := 100 // 100ms window for fast testing

	// First request
	entry1, expired1 := model.GetOrCreateEntry(key, windowMs)
	if expired1 {
		t.Error("First request should not report expired")
	}
	if entry1.Count != 1 {
		t.Errorf("First request count = %d, want 1", entry1.Count)
	}

	// Wait for window to expire
	time.Sleep(150 * time.Millisecond)

	// Next request should reset
	entry2, expired2 := model.GetOrCreateEntry(key, windowMs)
	if !expired2 {
		t.Error("Request after window should report expired")
	}
	if entry2.Count != 1 {
		t.Errorf("Request after window count = %d, want 1 (reset)", entry2.Count)
	}
}

// TestThrottlingModelEntry tests ThrottlingModelEntry struct
func TestThrottlingModelEntry(t *testing.T) {
	entry := ThrottlingModelEntry{
		L1:           "tenant-id",
		L2:           "192.168.1.1",
		FirstReqTime: time.Now(),
		Count:        10,
	}

	if entry.L1 != "tenant-id" {
		t.Errorf("L1 = %s, want tenant-id", entry.L1)
	}
	if entry.L2 != "192.168.1.1" {
		t.Errorf("L2 = %s, want 192.168.1.1", entry.L2)
	}
	if entry.Count != 10 {
		t.Errorf("Count = %d, want 10", entry.Count)
	}
}

// TestThrottlingErrorEntry tests ThrottlingErrorEntry struct
func TestThrottlingErrorEntry(t *testing.T) {
	now := time.Now()
	entry := ThrottlingErrorEntry{
		L1:        "tenant-id",
		L2:        "192.168.1.1",
		Strategy:  "defaultStrategy",
		Timestamp: now,
	}

	if entry.L1 != "tenant-id" {
		t.Errorf("L1 = %s, want tenant-id", entry.L1)
	}
	if entry.Strategy != "defaultStrategy" {
		t.Errorf("Strategy = %s, want defaultStrategy", entry.Strategy)
	}
	if !entry.Timestamp.Equal(now) {
		t.Errorf("Timestamp mismatch")
	}
}

// TestInitTrafficModel tests model initialization
func TestInitTrafficModel(t *testing.T) {
	// Save original state
	manager.mu.Lock()
	originalModel := manager.currentModel
	manager.mu.Unlock()

	// Test initializing with memory (no mongo client)
	InitTrafficModel(TrafficConfig{
		Model: "memory",
	})

	if GetCurrentModel() != "memory" {
		t.Errorf("Model = %s, want memory", GetCurrentModel())
	}

	// Test initializing with mongo but no client (should default to memory)
	InitTrafficModel(TrafficConfig{
		Model:       "mongo",
		MongoClient: nil,
	})

	if GetCurrentModel() != "memory" {
		t.Errorf("Model = %s, want memory (no client)", GetCurrentModel())
	}

	// Restore original state
	manager.mu.Lock()
	manager.currentModel = originalModel
	manager.mu.Unlock()
}

// BenchmarkMemoryThrottlingModel benchmarks the memory model
func BenchmarkMemoryThrottlingModel(b *testing.B) {
	model := NewMemoryThrottlingModel()
	key := TrafficKey{L1: "bench-tenant", L2: "192.168.1.1"}
	windowMs := 60000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model.GetOrCreateEntry(key, windowMs)
	}
}

// BenchmarkBuildTrafficKeyString benchmarks key string building
func BenchmarkBuildTrafficKeyString(b *testing.B) {
	key := TrafficKey{L1: "tenant123", L2: "192.168.1.1"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		BuildTrafficKeyString(key)
	}
}
