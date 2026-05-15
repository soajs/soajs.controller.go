package middleware

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
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

// TrafficConfig holds traffic middleware configuration
type TrafficConfig struct {
	Model       string // "memory" or "mongo"
	MongoClient *mongo.Client
	Database    string // database name for throttling collections (e.g., "avdevgateway")
	Logger      *zap.Logger
}

// throttlingManager manages the throttling model instance
type throttlingManager struct {
	mu           sync.RWMutex
	memoryModel  *MemoryThrottlingModel
	mongoModel   *MongoThrottlingModel
	currentModel string
	initialized  bool
}

var manager = &throttlingManager{}

func init() {
	// Initialize default memory model
	manager.memoryModel = NewMemoryThrottlingModel()
	manager.currentModel = "memory"
	manager.initialized = true

	// Start cleanup goroutine for memory model
	go func() {
		ticker := time.NewTicker(time.Minute)
		for range ticker.C {
			manager.mu.RLock()
			if manager.memoryModel != nil {
				manager.memoryModel.Cleanup(time.Hour)
			}
			if manager.mongoModel != nil {
				manager.mongoModel.Cleanup(time.Hour)
			}
			manager.mu.RUnlock()
		}
	}()
}

// InitTrafficModel initializes the throttling model based on configuration
// This should be called during server startup with the appropriate config
func InitTrafficModel(cfg TrafficConfig) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if cfg.Model == "mongo" && cfg.MongoClient != nil {
		logger := cfg.Logger
		if logger == nil {
			logger = zap.NewNop()
		}
		manager.mongoModel = NewMongoThrottlingModel(cfg.MongoClient, cfg.Database, logger)
		manager.currentModel = "mongo"
	} else {
		manager.currentModel = "memory"
	}
	manager.initialized = true
}

// getThrottlingModelFromConfig reads model config from registry
func getThrottlingModelFromConfig(regData *types.RegistryData) string {
	if regData == nil || regData.CustomRegistry == nil {
		return "memory"
	}

	gateway, ok := regData.CustomRegistry["gateway"]
	if !ok {
		return "memory"
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
		return "memory"
	}

	traffic, ok := gatewayValue["traffic"].(map[string]interface{})
	if !ok {
		return "memory"
	}

	if model, ok := traffic["model"].(string); ok {
		return model
	}

	return "memory"
}

// Traffic middleware handles rate limiting/throttling
func Traffic(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)
			logger := getLoggerFromContext(ctx)

			// Get throttling configuration
			if reg == nil {
				next.ServeHTTP(w, r)
				return
			}
			regData := reg.Get()
			if regData == nil || regData.ServiceConfig == nil || regData.ServiceConfig.Throttling == nil {
				if logger != nil {
					logger.Debug("throttling skipped: no throttling config",
						zap.Bool("regDataNil", regData == nil),
						zap.Bool("serviceConfigNil", regData != nil && regData.ServiceConfig == nil),
						zap.Bool("throttlingNil", regData != nil && regData.ServiceConfig != nil && regData.ServiceConfig.Throttling == nil),
					)
				}
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
				if logger != nil {
					logger.Debug("throttling skipped: no strategy found",
						zap.String("service", serviceName),
						zap.String("apiPath", apiPath),
						zap.Bool("isPublic", isPublic),
					)
				}
				next.ServeHTTP(w, r)
				return
			}

			// Get strategy config
			strategy := getStrategyConfig(regData.ServiceConfig.Throttling, strategyName)
			if strategy == nil || strategy.Status == 0 {
				if logger != nil {
					logger.Debug("throttling skipped: strategy disabled or not found",
						zap.String("strategyName", strategyName),
						zap.Bool("strategyNil", strategy == nil),
					)
				}
				next.ServeHTTP(w, r)
				return
			}

			if logger != nil {
				logger.Debug("throttling applied",
					zap.String("service", serviceName),
					zap.String("apiPath", apiPath),
					zap.String("strategy", strategyName),
					zap.Int("limit", strategy.Limit),
				)
			}

			// Build traffic key
			trafficKey := buildTrafficKeyStruct(soajsCtx.Tenant.ID, getClientIP(r), strategy.Type)

			// Check throttling using appropriate model
			result, headers := checkThrottlingWithModel(trafficKey, strategy, strategyName, 0)

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

	// Handle both map[string]interface{} and bson.M
	var gatewayMap map[string]interface{}
	switch v := gateway.(type) {
	case map[string]interface{}:
		gatewayMap = v
	case bson.M:
		gatewayMap = v
	default:
		return nil
	}

	// Get gateway value (could be nested under "value" key)
	var gatewayValue map[string]interface{}
	if val, exists := gatewayMap["value"]; exists {
		switch v := val.(type) {
		case map[string]interface{}:
			gatewayValue = v
		case bson.M:
			gatewayValue = v
		default:
			gatewayValue = gatewayMap
		}
	} else {
		gatewayValue = gatewayMap
	}

	// Get traffic config
	var traffic map[string]interface{}
	switch v := gatewayValue["traffic"].(type) {
	case map[string]interface{}:
		traffic = v
	case bson.M:
		traffic = v
	default:
		return nil
	}

	// Get throttling config
	var throttling map[string]interface{}
	switch v := traffic["throttling"].(type) {
	case map[string]interface{}:
		throttling = v
	case bson.M:
		throttling = v
	default:
		return nil
	}

	// Get service-specific throttling config
	var svcThrottling map[string]interface{}
	switch v := throttling[serviceName].(type) {
	case map[string]interface{}:
		svcThrottling = v
	case bson.M:
		svcThrottling = v
	default:
		return nil
	}

	// Check if API matches - handle both []interface{} and bson.A
	var apis []interface{}
	if a, ok := svcThrottling["apis"].([]interface{}); ok {
		apis = a
	} else if a, ok := svcThrottling["apis"].(bson.A); ok {
		apis = a
	}

	if apis != nil {
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

// buildTrafficKeyStruct creates a TrafficKey struct
func buildTrafficKeyStruct(tenantID, clientIP string, keyType int) TrafficKey {
	key := TrafficKey{
		L1: tenantID,
		L2: "_ALL_",
	}
	if keyType == 1 {
		// Tenant + IP
		key.L2 = clientIP
	}
	return key
}

// checkThrottlingWithModel checks if request is within limits using the configured model
func checkThrottlingWithModel(key TrafficKey, strategy *ThrottlingStrategy, strategyName string, retry int) (bool, map[string]string) {
	manager.mu.RLock()
	currentModel := manager.currentModel
	memModel := manager.memoryModel
	mongoModel := manager.mongoModel
	manager.mu.RUnlock()

	if currentModel == "mongo" && mongoModel != nil {
		return checkThrottlingMongo(mongoModel, key, strategy, strategyName, retry)
	}
	return checkThrottlingMemory(memModel, key, strategy, strategyName, retry)
}

// checkThrottlingMemory checks throttling using memory model
func checkThrottlingMemory(model *MemoryThrottlingModel, key TrafficKey, strategy *ThrottlingStrategy, strategyName string, retry int) (bool, map[string]string) {
	entry, windowExpired := model.GetOrCreateEntry(key, strategy.Window)

	// Calculate remaining time
	var remainingTimeMs int
	if windowExpired {
		remainingTimeMs = strategy.Window
	} else {
		remainingTime := model.GetRemainingTime(key, strategy.Window)
		remainingTimeMs = int(remainingTime.Milliseconds())
	}

	remaining := strategy.Limit - entry.Count
	headers := buildRateLimitHeaders(remainingTimeMs, strategy.Limit, remaining)

	if entry.Count > strategy.Limit {
		// Over limit, check retries
		if retry < strategy.Retries {
			// Sleep and retry
			time.Sleep(time.Duration(strategy.Delay) * time.Millisecond)
			return checkThrottlingMemory(model, key, strategy, strategyName, retry+1)
		}
		// All retries exhausted
		return false, headers
	}

	return true, headers
}

// checkThrottlingMongo checks throttling using MongoDB model
func checkThrottlingMongo(model *MongoThrottlingModel, key TrafficKey, strategy *ThrottlingStrategy, strategyName string, retry int) (bool, map[string]string) {
	entry, windowExpired, err := model.GetOrCreateEntry(key, strategy.Window)
	if err != nil {
		// On error, allow the request (fail open for availability)
		return true, buildRateLimitHeaders(strategy.Window, strategy.Limit, strategy.Limit)
	}

	// Calculate remaining time
	var remainingTimeMs int
	if windowExpired {
		remainingTimeMs = strategy.Window
	} else {
		remainingTime, _ := model.GetRemainingTime(key, strategy.Window)
		remainingTimeMs = int(remainingTime.Milliseconds())
	}

	remaining := strategy.Limit - entry.Count
	headers := buildRateLimitHeaders(remainingTimeMs, strategy.Limit, remaining)

	if entry.Count > strategy.Limit {
		// Over limit, check retries
		if retry < strategy.Retries {
			// Sleep and retry
			time.Sleep(time.Duration(strategy.Delay) * time.Millisecond)
			return checkThrottlingMongo(model, key, strategy, strategyName, retry+1)
		}
		// All retries exhausted - log the error
		_ = model.LogTooManyRequests(key, strategyName)
		return false, headers
	}

	return true, headers
}

// buildRateLimitHeaders creates rate limit response headers
func buildRateLimitHeaders(remainingMs, limit, remaining int) map[string]string {
	if remaining < 0 {
		remaining = 0
	}
	retryAfter := remainingMs / 1000
	if retryAfter < 1 {
		retryAfter = 1
	}
	return map[string]string{
		"Retry-After":           strconv.Itoa(retryAfter),
		"X-RateLimit-Limit":     strconv.Itoa(limit),
		"X-RateLimit-Remaining": strconv.Itoa(remaining),
	}
}

// SetRateLimit sets a custom rate limit for a key (kept for compatibility)
func SetRateLimit(key string, rps int, burst int) {
	// Not used in new implementation
}

// GetCurrentModel returns the current throttling model type (for testing/debugging)
func GetCurrentModel() string {
	manager.mu.RLock()
	defer manager.mu.RUnlock()
	return manager.currentModel
}
