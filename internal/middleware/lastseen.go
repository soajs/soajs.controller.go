package middleware

import (
	"net/http"
	"sync"
	"time"
)

// lastSeenStore tracks activity timestamps
type lastSeenStore struct {
	mu    sync.RWMutex
	data  map[string]time.Time
}

var lastSeen = &lastSeenStore{
	data: make(map[string]time.Time),
}

// LastSeen middleware tracks the last activity time for keys/users
func LastSeen() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// Track activity
			if soajsCtx != nil {
				var trackingKey string

				// Prefer tracking by user ID if available
				if soajsCtx.Controller != nil &&
					soajsCtx.Controller.ServiceParams != nil &&
					soajsCtx.Controller.ServiceParams.UserInfo != nil {
					if userID, ok := soajsCtx.Controller.ServiceParams.UserInfo["_id"].(string); ok {
						trackingKey = "user:" + userID
					}
				}

				// Fall back to tracking by key
				if trackingKey == "" && soajsCtx.Key != nil {
					trackingKey = "key:" + soajsCtx.Key.ExtKey
				}

				// Fall back to tracking by tenant
				if trackingKey == "" && soajsCtx.Tenant != nil {
					trackingKey = "tenant:" + soajsCtx.Tenant.ID
				}

				if trackingKey != "" {
					updateLastSeen(trackingKey)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

func updateLastSeen(key string) {
	lastSeen.mu.Lock()
	defer lastSeen.mu.Unlock()
	lastSeen.data[key] = time.Now()
}

// GetLastSeen returns the last seen time for a key
func GetLastSeen(key string) (time.Time, bool) {
	lastSeen.mu.RLock()
	defer lastSeen.mu.RUnlock()
	t, exists := lastSeen.data[key]
	return t, exists
}

// GetAllLastSeen returns all last seen data
func GetAllLastSeen() map[string]time.Time {
	lastSeen.mu.RLock()
	defer lastSeen.mu.RUnlock()

	result := make(map[string]time.Time, len(lastSeen.data))
	for k, v := range lastSeen.data {
		result[k] = v
	}
	return result
}

// CleanupLastSeen removes entries older than the given duration
func CleanupLastSeen(maxAge time.Duration) {
	lastSeen.mu.Lock()
	defer lastSeen.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for key, t := range lastSeen.data {
		if t.Before(cutoff) {
			delete(lastSeen.data, key)
		}
	}
}
