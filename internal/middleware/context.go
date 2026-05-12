package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// SOAJSContext holds all SOAJS-specific request data
type SOAJSContext struct {
	Log            *zap.Logger
	Controller     *ControllerContext
	Tenant         *types.Tenant
	Key            *types.Key
	ServicesConfig map[string]interface{}
	Awareness      AwarenessClient
	AwarenessEnv   AwarenessEnvClient
	InputMask      interface{}
	Device         *types.DeviceInfo
	Geo            *types.GeoInfo
}

// ControllerContext holds gateway-specific request data
type ControllerContext struct {
	ServiceParams     *types.ServiceParams
	Gotoservice       func(http.ResponseWriter, *http.Request)
	RedirectedRequest *http.Request
}

// AwarenessClient interface for service discovery
type AwarenessClient interface {
	GetHost(serviceName string, version string) (string, error)
}

// AwarenessEnvClient interface for environment-aware service discovery
type AwarenessEnvClient interface {
	GetHost(env string, serviceName string, version string) (string, error)
}

// Context middleware initializes the SOAJS context for each request
func Context(logger *zap.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Generate request ID if not present
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				requestID = uuid.New().String()
			}

			// Create request-scoped logger
			reqLogger := logger.With(
				zap.String("request_id", requestID),
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
			)

			// Initialize SOAJS context
			soajsCtx := &SOAJSContext{
				Log: reqLogger,
				Controller: &ControllerContext{
					ServiceParams: &types.ServiceParams{},
				},
				ServicesConfig: make(map[string]interface{}),
			}

			// Add to request context
			ctx := r.Context()
			ctx = context.WithValue(ctx, config.CtxKeySOAJS, soajsCtx)
			ctx = context.WithValue(ctx, config.CtxKeyRequestID, requestID)
			ctx = context.WithValue(ctx, config.CtxKeyStartTime, time.Now())

			// Set response header
			w.Header().Set("X-Request-ID", requestID)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// GetSOAJSContext retrieves the SOAJS context from request context
func GetSOAJSContext(ctx context.Context) *SOAJSContext {
	if v := ctx.Value(config.CtxKeySOAJS); v != nil {
		return v.(*SOAJSContext)
	}
	return nil
}

// GetRequestID retrieves the request ID from context
func GetRequestID(ctx context.Context) string {
	if v := ctx.Value(config.CtxKeyRequestID); v != nil {
		return v.(string)
	}
	return ""
}

// GetStartTime retrieves the request start time from context
func GetStartTime(ctx context.Context) time.Time {
	if v := ctx.Value(config.CtxKeyStartTime); v != nil {
		return v.(time.Time)
	}
	return time.Time{}
}
