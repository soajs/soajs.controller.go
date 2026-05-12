package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"golang.org/x/time/rate"
)

// TrafficConfig holds traffic management configuration
type TrafficConfig struct {
	Enabled        bool
	DefaultLimit   int
	DefaultBurst   int
	CleanupInterval time.Duration
}

// DefaultTrafficConfig returns default traffic configuration
func DefaultTrafficConfig() TrafficConfig {
	return TrafficConfig{
		Enabled:        true,
		DefaultLimit:   100,  // requests per second
		DefaultBurst:   200,  // burst size
		CleanupInterval: 10 * time.Minute,
	}
}

// rateLimiterStore manages per-key rate limiters
type rateLimiterStore struct {
	mu       sync.RWMutex
	limiters map[string]*rateLimiterEntry
	config   TrafficConfig
}

type rateLimiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

var trafficStore = &rateLimiterStore{
	limiters: make(map[string]*rateLimiterEntry),
	config:   DefaultTrafficConfig(),
}

func init() {
	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(trafficStore.config.CleanupInterval)
		for range ticker.C {
			trafficStore.cleanup()
		}
	}()
}

// Traffic middleware handles rate limiting
func Traffic(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// Determine rate limit key
			var limitKey string
			if soajsCtx != nil && soajsCtx.Key != nil {
				// Use API key for rate limiting
				limitKey = soajsCtx.Key.ExtKey
			} else {
				// Fall back to IP-based limiting
				limitKey = "ip:" + getClientIP(r)
			}

			// Get or create rate limiter
			limiter := trafficStore.getLimiter(limitKey)

			// Check rate limit
			if !limiter.Allow() {
				WriteError(w, http.StatusTooManyRequests,
					config.ErrCodeRateLimitExceeded,
					config.DefaultErrors[config.ErrCodeRateLimitExceeded])
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func (s *rateLimiterStore) getLimiter(key string) *rate.Limiter {
	s.mu.RLock()
	entry, exists := s.limiters[key]
	s.mu.RUnlock()

	if exists {
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	// Create new limiter
	s.mu.Lock()
	defer s.mu.Unlock()

	// Double-check after acquiring write lock
	if entry, exists := s.limiters[key]; exists {
		entry.lastSeen = time.Now()
		return entry.limiter
	}

	limiter := rate.NewLimiter(rate.Limit(s.config.DefaultLimit), s.config.DefaultBurst)
	s.limiters[key] = &rateLimiterEntry{
		limiter:  limiter,
		lastSeen: time.Now(),
	}

	return limiter
}

func (s *rateLimiterStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-s.config.CleanupInterval)
	for key, entry := range s.limiters {
		if entry.lastSeen.Before(cutoff) {
			delete(s.limiters, key)
		}
	}
}

// SetRateLimit sets a custom rate limit for a key
func SetRateLimit(key string, rps int, burst int) {
	trafficStore.mu.Lock()
	defer trafficStore.mu.Unlock()

	trafficStore.limiters[key] = &rateLimiterEntry{
		limiter:  rate.NewLimiter(rate.Limit(rps), burst),
		lastSeen: time.Now(),
	}
}
