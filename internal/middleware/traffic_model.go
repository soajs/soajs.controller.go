package middleware

import (
	"time"
)

// TrafficKey represents the key for throttling (tenant + optional IP)
type TrafficKey struct {
	L1 string // tenant ID
	L2 string // client IP or "_ALL_" for tenant-only
}

// ThrottlingModelEntry represents a throttling entry in storage
type ThrottlingModelEntry struct {
	L1           string    `bson:"l1" json:"l1"`
	L2           string    `bson:"l2" json:"l2"`
	FirstReqTime time.Time `bson:"firstReqTime" json:"firstReqTime"`
	Count        int       `bson:"count" json:"count"`
}

// ThrottlingErrorEntry represents an error log entry
type ThrottlingErrorEntry struct {
	L1        string    `bson:"l1" json:"l1"`
	L2        string    `bson:"l2" json:"l2"`
	Strategy  string    `bson:"strategy" json:"strategy"`
	Timestamp time.Time `bson:"timestamp" json:"timestamp"`
}

// ThrottlingModel defines the interface for throttling storage backends
type ThrottlingModel interface {
	// GetThrottling retrieves the current throttling entry for a key
	GetThrottling(key TrafficKey) (*ThrottlingModelEntry, error)

	// ResetThrottling resets the throttling counter for a key
	ResetThrottling(key TrafficKey) error

	// IncrementThrottling increments the request count for a key
	// If the entry doesn't exist, it creates one with count=1
	IncrementThrottling(key TrafficKey) error

	// LogTooManyRequests logs a rejected request to the error collection
	LogTooManyRequests(key TrafficKey, strategy string) error

	// Cleanup removes expired entries (for memory model)
	Cleanup(maxAge time.Duration)
}

// BuildTrafficKeyFromString parses a traffic key string back to TrafficKey
// Format: "tenant:{tenantId}" or "tenant:{tenantId}:ip:{ip}"
func BuildTrafficKeyFromString(key string) TrafficKey {
	// This is kept for backward compatibility with existing key format
	tk := TrafficKey{L2: "_ALL_"}

	// Parse "tenant:{id}" or "tenant:{id}:ip:{ip}"
	if len(key) > 7 && key[:7] == "tenant:" {
		rest := key[7:]
		for i, c := range rest {
			if c == ':' {
				tk.L1 = rest[:i]
				// Check for ":ip:" pattern
				if len(rest) > i+4 && rest[i:i+4] == ":ip:" {
					tk.L2 = rest[i+4:]
				}
				return tk
			}
		}
		tk.L1 = rest
	}

	return tk
}

// BuildTrafficKeyString creates a string key from TrafficKey
func BuildTrafficKeyString(key TrafficKey) string {
	if key.L2 == "_ALL_" || key.L2 == "" {
		return "tenant:" + key.L1
	}
	return "tenant:" + key.L1 + ":ip:" + key.L2
}
