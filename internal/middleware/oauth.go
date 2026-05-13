package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
)

// OAuthConfig holds OAuth middleware configuration
type OAuthConfig struct {
	Enabled       bool
	TokenEndpoint string
}

// OAuth middleware validates OAuth tokens
func OAuth(prov provision.Provision) Middleware {
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

			// Validate token using provision's OAuth model
			oauthModel := prov.OAuthModel()
			accessToken, err := oauthModel.GetAccessToken(token)
			if err != nil {
				soajsCtx.Log.Debug("OAuth token lookup failed")
				WriteError(w, http.StatusUnauthorized,
					config.ErrCodeUnauthorized,
					"Invalid or expired token")
				return
			}

			// Check if token is expired
			if !accessToken.Expires.IsZero() && accessToken.Expires.Before(time.Now()) {
				soajsCtx.Log.Debug("OAuth token expired")
				WriteError(w, http.StatusUnauthorized,
					config.ErrCodeUnauthorized,
					"Token has expired")
				return
			}

			// Store user info in context
			userInfo := make(map[string]interface{})
			if accessToken.User != nil {
				userInfo = accessToken.User
			}
			userInfo["_id"] = accessToken.UserID
			userInfo["clientId"] = accessToken.ClientID
			userInfo["scope"] = accessToken.Scope

			soajsCtx.Controller.ServiceParams.UserInfo = userInfo
			soajsCtx.Controller.ServiceParams.OAuthToken = accessToken

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
