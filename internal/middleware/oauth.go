package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// OAuthConfig holds OAuth middleware configuration
type OAuthConfig struct {
	Enabled       bool
	TokenEndpoint string
}

// JWTClaims represents decoded JWT claims
type JWTClaims struct {
	Sub      string                 `json:"sub"`
	Aud      string                 `json:"aud"`
	Exp      int64                  `json:"exp"`
	Iat      int64                  `json:"iat"`
	Iss      string                 `json:"iss"`
	UserData map[string]interface{} `json:"-"`
}

// OAuth middleware validates OAuth tokens (supports both OAuth2 and JWT)
func OAuth(prov provision.Provision) Middleware {
	return OAuthWithRegistry(prov, nil)
}

// OAuthWithRegistry middleware validates OAuth tokens with registry access
func OAuthWithRegistry(prov provision.Provision, reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx == nil || soajsCtx.Controller == nil ||
				soajsCtx.Controller.ServiceParams == nil {
				next.ServeHTTP(w, r)
				return
			}

			requiresOAuth := soajsCtx.Controller.ServiceParams.RequiresOAuth

			// Extract token
			token := extractToken(r)
			if token == "" {
				// No token: if OAuth is required, return 401; otherwise continue
				if requiresOAuth {
					WriteError(w, http.StatusUnauthorized,
						config.ErrCodeUnauthorized,
						config.DefaultErrors[config.ErrCodeUnauthorized])
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Token found - always process it (even if OAuth not required)
			// This allows optional OAuth for endpoints like /soajs/acl

			// Determine OAuth type (0=JWT, 2=OAuth2)
			oauthType := 2
			var jwtSecret string
			var jwtAlgorithms []string
			var jwtAudience string

			// Check tenant OAuth config first
			if soajsCtx.Tenant != nil {
				// Get tenant OAuth settings from ServicesConfig if available
				if soajsCtx.ServicesConfig != nil {
					if tenantOAuth, ok := soajsCtx.ServicesConfig["oauth"].(map[string]interface{}); ok {
						if typ, ok := tenantOAuth["type"].(float64); ok {
							oauthType = int(typ)
						}
						if secret, ok := tenantOAuth["secret"].(string); ok {
							jwtSecret = secret
						}
					}
				}
			}

			// Fall back to registry config
			if reg != nil {
				regData := reg.Get()
				if regData != nil && regData.ServiceConfig != nil && regData.ServiceConfig.OAuth.Grants != nil {
					// Check OAuth type from registry
					// Note: OAuth config in registry doesn't have a type field in the current types
					// Use the tenant override or default to type 2
				}
			}

			if oauthType == 0 {
				// JWT validation (Type 0)
				if jwtAlgorithms == nil {
					jwtAlgorithms = []string{"HS256"}
				}

				claims, err := validateJWT(token, jwtSecret, jwtAlgorithms, jwtAudience)
				if err != nil {
					soajsCtx.Log.Debug("JWT validation failed")
					WriteError(w, http.StatusUnauthorized,
						config.ErrCodeUnauthorized,
						"Invalid or expired token")
					return
				}

				// Store user info from JWT claims
				userInfo := make(map[string]interface{})
				userInfo["_id"] = claims.Sub
				if claims.UserData != nil {
					for k, v := range claims.UserData {
						userInfo[k] = v
					}
				}

				soajsCtx.Controller.ServiceParams.UserInfo = userInfo
			} else {
				// OAuth2 validation (Type 2)
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
				// The token's user object contains the full user profile with groupsConfig
				// Convert from BSON types to native Go types for proper type assertions
				userInfo := make(map[string]interface{})
				if accessToken.User != nil {
					userInfo = convertBSONMap(accessToken.User)
				}

				// Get user ID: prefer top-level userId, fallback to user._id
				userID := accessToken.UserID
				if userID == "" {
					if id, ok := userInfo["_id"].(string); ok {
						userID = id
					}
				}
				userInfo["_id"] = userID
				userInfo["clientId"] = accessToken.ClientID
				userInfo["scope"] = accessToken.Scope

				soajsCtx.Controller.ServiceParams.UserInfo = userInfo
				soajsCtx.Controller.ServiceParams.OAuthToken = accessToken
			}

			next.ServeHTTP(w, r)
		})
	}
}

// validateJWT validates a JWT token and returns claims
func validateJWT(tokenStr, secret string, algorithms []string, expectedAudience string) (*JWTClaims, error) {
	// Split the token into parts
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, errInvalidJWT
	}

	// Decode header
	headerBytes, err := base64URLDecode(parts[0])
	if err != nil {
		return nil, err
	}

	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return nil, err
	}

	// Verify algorithm is allowed
	algAllowed := false
	for _, alg := range algorithms {
		if alg == header.Alg {
			algAllowed = true
			break
		}
	}
	if !algAllowed {
		return nil, errInvalidJWT
	}

	// Decode payload
	payloadBytes, err := base64URLDecode(parts[1])
	if err != nil {
		return nil, err
	}

	var claims JWTClaims
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		return nil, err
	}

	// Also capture any extra fields
	var allClaims map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &allClaims); err == nil {
		claims.UserData = allClaims
	}

	// Verify signature (HS256)
	if header.Alg == "HS256" {
		message := parts[0] + "." + parts[1]
		signature, err := base64URLDecode(parts[2])
		if err != nil {
			return nil, err
		}

		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(message))
		expectedSig := h.Sum(nil)

		if !hmac.Equal(signature, expectedSig) {
			return nil, errInvalidJWT
		}
	}

	// Check expiration
	if claims.Exp > 0 && time.Now().Unix() > claims.Exp {
		return nil, errTokenExpired
	}

	// Check audience if required
	if expectedAudience != "" && claims.Aud != expectedAudience {
		return nil, errInvalidJWT
	}

	return &claims, nil
}

var errInvalidJWT = &jwtError{"invalid JWT"}
var errTokenExpired = &jwtError{"token expired"}

type jwtError struct {
	msg string
}

func (e *jwtError) Error() string {
	return e.msg
}

// base64URLDecode decodes base64url encoded string
func base64URLDecode(s string) ([]byte, error) {
	// Add padding if needed
	switch len(s) % 4 {
	case 2:
		s += "=="
	case 3:
		s += "="
	}
	return base64.URLEncoding.DecodeString(s)
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

// convertBSONMap recursively converts BSON types to native Go map[string]interface{}
// This ensures type assertions like map["key"].(map[string]interface{}) work correctly
func convertBSONMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{})
	for k, v := range m {
		result[k] = convertBSONValue(v)
	}
	return result
}

// convertBSONValue recursively converts a BSON value to native Go types
func convertBSONValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	// Convert ObjectID to hex string
	if oid, ok := v.(primitive.ObjectID); ok {
		return oid.Hex()
	}
	// Convert bson.D (ordered document) to map
	if d, ok := v.(bson.D); ok {
		result := make(map[string]interface{})
		for _, elem := range d {
			result[elem.Key] = convertBSONValue(elem.Value)
		}
		return result
	}
	// Convert bson.M to map (recursively convert values)
	if m, ok := v.(bson.M); ok {
		result := make(map[string]interface{})
		for k, val := range m {
			result[k] = convertBSONValue(val)
		}
		return result
	}
	// Convert map[string]interface{} (recursively convert values)
	if m, ok := v.(map[string]interface{}); ok {
		result := make(map[string]interface{})
		for k, val := range m {
			result[k] = convertBSONValue(val)
		}
		return result
	}
	// Convert bson.A (array) to slice
	if a, ok := v.(bson.A); ok {
		result := make([]interface{}, len(a))
		for i, val := range a {
			result[i] = convertBSONValue(val)
		}
		return result
	}
	// Convert []interface{} (recursively convert values)
	if a, ok := v.([]interface{}); ok {
		result := make([]interface{}, len(a))
		for i, val := range a {
			result[i] = convertBSONValue(val)
		}
		return result
	}
	// Return primitives as-is
	return v
}
