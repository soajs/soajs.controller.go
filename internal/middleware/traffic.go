package middleware

import (
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// ThrottlingStrategy holds throttling strategy configuration
type ThrottlingStrategy struct {
	Status  int `json:"status"`  // 0=Off, 1=On
	Type    int `json:"type"`    // 0=tenant, 1=tenant+ip
	Window  int `json:"window"`  // milliseconds
	Limit   int `json:"limit"`   // max requests
	Retries int `json:"retries"` // retry attempts
	Delay   int `json:"delay"`   // retry delay in ms
}

// ThrottlingEntry tracks request counts for a key
type ThrottlingEntry struct {
	Count        int
	FirstReqTime time.Time
}

// ThrottlingStore manages throttling entries
type ThrottlingStore struct {
	mu      sync.RWMutex
	entries map[string]*ThrottlingEntry
}

var throttlingStore = &ThrottlingStore{
	entries: make(map[string]*ThrottlingEntry),
}

func init() {
	// Start cleanup goroutine
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			throttlingStore.cleanup()
		}
	}()
}

// Traffic middleware handles rate limiting/throttling
func Traffic(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// Get throttling configuration
			if reg == nil {
				next.ServeHTTP(w, r)
				return
			}
			regData := reg.Get()
			if regData == nil || regData.ServiceConfig == nil || regData.ServiceConfig.Throttling == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Check if tenant is available
			if soajsCtx == nil || soajsCtx.Tenant == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Get service info
			serviceName := ""
			apiPath := ""
			isPublic := false

			if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
				serviceName = soajsCtx.Controller.ServiceParams.ServiceName
				if soajsCtx.Controller.ServiceParams.URLInfo != nil {
					apiPath = soajsCtx.Controller.ServiceParams.URLInfo.ServicePath
				}
				// Check if API is public (no auth required)
				isPublic = !soajsCtx.Controller.ServiceParams.RequiresOAuth
			}

			// Determine strategy
			strategyName := getThrottlingStrategy(regData, soajsCtx, serviceName, apiPath, isPublic)
			if strategyName == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Get strategy config
			strategy := getStrategyConfig(regData.ServiceConfig.Throttling, strategyName)
			if strategy == nil || strategy.Status == 0 {
				next.ServeHTTP(w, r)
				return
			}

			// Build traffic key
			trafficKey := buildTrafficKey(soajsCtx.Tenant.ID, getClientIP(r), strategy.Type)

			// Check throttling
			result, headers := checkThrottling(trafficKey, strategy, 0)

			// Set rate limit headers
			for k, v := range headers {
				w.Header().Set(k, v)
			}

			if !result {
				w.WriteHeader(http.StatusTooManyRequests)
				WriteError(w, http.StatusTooManyRequests,
					config.ErrCodeRateLimitExceeded,
					"Too many requests")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getThrottlingStrategy determines which throttling strategy to use
func getThrottlingStrategy(regData *types.RegistryData, soajsCtx *SOAJSContext, serviceName, apiPath string, isPublic bool) string {
	strategyKey := "privateAPIStrategy"
	if isPublic {
		strategyKey = "publicAPIStrategy"
	}

	// Check custom registry for service-specific overrides
	if regData.CustomRegistry != nil {
		throttlingConfig := getThrottlingConfig(regData.CustomRegistry, serviceName, apiPath)
		if throttlingConfig != nil {
			if strategy, ok := throttlingConfig[strategyKey].(string); ok {
				return strategy
			}
		}
	}

	// Check services config for tenant-specific overrides
	if soajsCtx.ServicesConfig != nil {
		if gateway, ok := soajsCtx.ServicesConfig["gateway"].(map[string]interface{}); ok {
			if throttling, ok := gateway["throttling"].(map[string]interface{}); ok {
				if svcThrottling, ok := throttling[serviceName].(map[string]interface{}); ok {
					if strategy, ok := svcThrottling[strategyKey].(string); ok {
						return strategy
					}
				}
				if strategy, ok := throttling[strategyKey].(string); ok {
					return strategy
				}
			}
		}
	}

	// Use default from registry
	if regData.ServiceConfig.Throttling != nil {
		if strategy, ok := regData.ServiceConfig.Throttling[strategyKey].(string); ok {
			return strategy
		}
	}

	return ""
}

// getThrottlingConfig extracts throttling config from custom registry
func getThrottlingConfig(customRegistry map[string]interface{}, serviceName, apiPath string) map[string]interface{} {
	gateway, ok := customRegistry["gateway"]
	if !ok {
		return nil
	}

	var gatewayValue map[string]interface{}
	switch v := gateway.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			gatewayValue, _ = val.(map[string]interface{})
		} else {
			gatewayValue = v
		}
	default:
		return nil
	}

	traffic, ok := gatewayValue["traffic"].(map[string]interface{})
	if !ok {
		return nil
	}

	throttling, ok := traffic["throttling"].(map[string]interface{})
	if !ok {
		return nil
	}

	svcThrottling, ok := throttling[serviceName].(map[string]interface{})
	if !ok {
		return nil
	}

	// Check if API matches
	if apis, ok := svcThrottling["apis"].([]interface{}); ok {
		apiMatched := false
		for _, api := range apis {
			if apiStr, ok := api.(string); ok && apiStr == apiPath {
				apiMatched = true
				break
			}
		}
		if !apiMatched {
			return nil
		}
	}

	return svcThrottling
}

// getStrategyConfig extracts strategy configuration
func getStrategyConfig(throttling map[string]interface{}, strategyName string) *ThrottlingStrategy {
	strategyData, ok := throttling[strategyName]
	if !ok {
		return nil
	}

	strategyMap, ok := strategyData.(map[string]interface{})
	if !ok {
		return nil
	}

	strategy := &ThrottlingStrategy{
		Status:  1,
		Type:    1,
		Window:  60000,
		Limit:   50,
		Retries: 2,
		Delay:   1000,
	}

	if status, ok := strategyMap["status"].(float64); ok {
		strategy.Status = int(status)
	}
	if typ, ok := strategyMap["type"].(float64); ok {
		strategy.Type = int(typ)
	}
	if window, ok := strategyMap["window"].(float64); ok {
		strategy.Window = int(window)
	}
	if limit, ok := strategyMap["limit"].(float64); ok {
		strategy.Limit = int(limit)
	}
	if retries, ok := strategyMap["retries"].(float64); ok {
		strategy.Retries = int(retries)
	}
	if delay, ok := strategyMap["delay"].(float64); ok {
		strategy.Delay = int(delay)
	}

	return strategy
}

// buildTrafficKey creates the throttling key
func buildTrafficKey(tenantID, clientIP string, keyType int) string {
	if keyType == 0 {
		// Tenant only
		return fmt.Sprintf("tenant:%s", tenantID)
	}
	// Tenant + IP
	return fmt.Sprintf("tenant:%s:ip:%s", tenantID, clientIP)
}

// checkThrottling checks if request is within limits
func checkThrottling(key string, strategy *ThrottlingStrategy, retry int) (bool, map[string]string) {
	throttlingStore.mu.Lock()
	defer throttlingStore.mu.Unlock()

	now := time.Now()
	entry, exists := throttlingStore.entries[key]

	if !exists {
		// First request
		throttlingStore.entries[key] = &ThrottlingEntry{
			Count:        1,
			FirstReqTime: now,
		}
		remaining := strategy.Limit - 1
		return true, buildRateLimitHeaders(strategy.Window, strategy.Limit, remaining)
	}

	// Check if window has expired
	windowDuration := time.Duration(strategy.Window) * time.Millisecond
	remainingTime := windowDuration - now.Sub(entry.FirstReqTime)

	if remainingTime <= 0 {
		// Window expired, reset
		entry.Count = 1
		entry.FirstReqTime = now
		remaining := strategy.Limit - 1
		return true, buildRateLimitHeaders(strategy.Window, strategy.Limit, remaining)
	}

	// Increment count
	if retry == 0 {
		entry.Count++
	}

	remaining := strategy.Limit - entry.Count
	headers := buildRateLimitHeaders(int(remainingTime.Milliseconds()), strategy.Limit, remaining)

	if entry.Count > strategy.Limit {
		// Over limit, check retries
		if retry < strategy.Retries {
			// Sleep and retry
			time.Sleep(time.Duration(strategy.Delay) * time.Millisecond)
			return checkThrottling(key, strategy, retry+1)
		}
		// All retries exhausted
		return false, headers
	}

	return true, headers
}

// buildRateLimitHeaders creates rate limit response headers
func buildRateLimitHeaders(remainingMs, limit, remaining int) map[string]string {
	if remaining < 0 {
		remaining = 0
	}
	return map[string]string{
		"Retry-After":          strconv.Itoa(remainingMs / 1000),
		"X-RateLimit-Limit":    strconv.Itoa(limit),
		"X-RateLimit-Remaining": strconv.Itoa(remaining),
	}
}

// cleanup removes expired entries
func (s *ThrottlingStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-time.Hour)
	for key, entry := range s.entries {
		if entry.FirstReqTime.Before(cutoff) {
			delete(s.entries, key)
		}
	}
}

// SetRateLimit sets a custom rate limit for a key (kept for compatibility)
func SetRateLimit(key string, rps int, burst int) {
	// Not used in new implementation
}
