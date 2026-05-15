package middleware

import (
	"sync"
	"time"
)

// MemoryThrottlingModel implements ThrottlingModel using in-memory storage
type MemoryThrottlingModel struct {
	mu      sync.RWMutex
	entries map[string]*ThrottlingModelEntry
}

// NewMemoryThrottlingModel creates a new memory-based throttling model
func NewMemoryThrottlingModel() *MemoryThrottlingModel {
	return &MemoryThrottlingModel{
		entries: make(map[string]*ThrottlingModelEntry),
	}
}

// GetThrottling retrieves the current throttling entry for a key
func (m *MemoryThrottlingModel) GetThrottling(key TrafficKey) (*ThrottlingModelEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keyStr := BuildTrafficKeyString(key)
	entry, exists := m.entries[keyStr]
	if !exists {
		return nil, nil
	}

	// Return a copy to avoid race conditions
	return &ThrottlingModelEntry{
		L1:           entry.L1,
		L2:           entry.L2,
		FirstReqTime: entry.FirstReqTime,
		Count:        entry.Count,
	}, nil
}

// ResetThrottling resets the throttling counter for a key
func (m *MemoryThrottlingModel) ResetThrottling(key TrafficKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	keyStr := BuildTrafficKeyString(key)
	if entry, exists := m.entries[keyStr]; exists {
		entry.Count = 1
		entry.FirstReqTime = time.Now()
	}

	return nil
}

// IncrementThrottling increments the request count for a key
func (m *MemoryThrottlingModel) IncrementThrottling(key TrafficKey) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	keyStr := BuildTrafficKeyString(key)
	if entry, exists := m.entries[keyStr]; exists {
		entry.Count++
	} else {
		m.entries[keyStr] = &ThrottlingModelEntry{
			L1:           key.L1,
			L2:           key.L2,
			FirstReqTime: time.Now(),
			Count:        1,
		}
	}

	return nil
}

// LogTooManyRequests logs a rejected request (no-op for memory model)
func (m *MemoryThrottlingModel) LogTooManyRequests(key TrafficKey, strategy string) error {
	// Memory model doesn't persist error logs
	return nil
}

// Cleanup removes expired entries
func (m *MemoryThrottlingModel) Cleanup(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for keyStr, entry := range m.entries {
		if entry.FirstReqTime.Before(cutoff) {
			delete(m.entries, keyStr)
		}
	}
}

// GetOrCreateEntry gets an existing entry or creates a new one atomically
// Returns the entry and whether the window has expired
func (m *MemoryThrottlingModel) GetOrCreateEntry(key TrafficKey, windowMs int) (*ThrottlingModelEntry, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	keyStr := BuildTrafficKeyString(key)
	now := time.Now()
	windowDuration := time.Duration(windowMs) * time.Millisecond

	entry, exists := m.entries[keyStr]
	if !exists {
		// First request
		m.entries[keyStr] = &ThrottlingModelEntry{
			L1:           key.L1,
			L2:           key.L2,
			FirstReqTime: now,
			Count:        1,
		}
		return m.entries[keyStr], false
	}

	// Check if window expired
	if now.Sub(entry.FirstReqTime) >= windowDuration {
		// Reset window
		entry.Count = 1
		entry.FirstReqTime = now
		return entry, true
	}

	// Increment within window
	entry.Count++
	return entry, false
}

// GetRemainingTime returns the remaining time in the current window
func (m *MemoryThrottlingModel) GetRemainingTime(key TrafficKey, windowMs int) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keyStr := BuildTrafficKeyString(key)
	entry, exists := m.entries[keyStr]
	if !exists {
		return 0
	}

	windowDuration := time.Duration(windowMs) * time.Millisecond
	elapsed := time.Since(entry.FirstReqTime)
	remaining := windowDuration - elapsed

	if remaining < 0 {
		return 0
	}
	return remaining
}
