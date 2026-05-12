package middleware

import (
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/config"
)

// OAuthConfig holds OAuth middleware configuration
type OAuthConfig struct {
	Enabled       bool
	TokenEndpoint string
}

// OAuth middleware validates OAuth tokens
func OAuth() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// Check if OAuth is required
			if soajsCtx == nil || soajsCtx.Controller == nil ||
				soajsCtx.Controller.ServiceParams == nil ||
				!soajsCtx.Controller.ServiceParams.RequiresOAuth {
				next.ServeHTTP(w, r)
				return
			}

			// Extract token
			token := extractToken(r)
			if token == "" {
				WriteError(w, http.StatusUnauthorized,
					config.ErrCodeUnauthorized,
					config.DefaultErrors[config.ErrCodeUnauthorized])
				return
			}

			// Validate token
			// TODO: Implement actual token validation against OAuth server
			valid, userInfo := validateToken(token)
			if !valid {
				WriteError(w, http.StatusUnauthorized,
					config.ErrCodeUnauthorized,
					"Invalid or expired token")
				return
			}

			// Store user info in context
			if userInfo != nil {
				soajsCtx.Controller.ServiceParams.UserInfo = userInfo
			}

			next.ServeHTTP(w, r)
		})
	}
}

func extractToken(r *http.Request) string {
	// Check Authorization header
	auth := r.Header.Get(config.HeaderAuthorization)
	if auth != "" {
		// Bearer token
		if strings.HasPrefix(auth, "Bearer ") {
			return strings.TrimPrefix(auth, "Bearer ")
		}
		return auth
	}

	// Check access_token header
	if token := r.Header.Get(config.HeaderSOAJSAccessToken); token != "" {
		return token
	}

	// Check query parameter
	if token := r.URL.Query().Get("access_token"); token != "" {
		return token
	}

	return ""
}

// validateToken validates the OAuth token
// TODO: Implement actual validation against OAuth server
func validateToken(token string) (bool, map[string]interface{}) {
	// Placeholder implementation
	// In real implementation, this would:
	// 1. Check token in local cache
	// 2. If not cached, validate against OAuth server
	// 3. Cache the result with TTL
	// 4. Return user info from token

	if token == "" {
		return false, nil
	}

	// For now, just return true for non-empty tokens
	// Real implementation would validate against OAuth service
	return true, map[string]interface{}{
		"token": token,
	}
}
