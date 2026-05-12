package middleware

import (
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
)

// Key middleware extracts and validates the API key from the request
func Key(prov provision.Provision) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// Extract key from header or query parameter
			key := r.Header.Get(config.HeaderSOAJSKey)
			if key == "" {
				key = r.URL.Query().Get("key")
			}

			// If no key provided, continue (some routes may not require key)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Validate and get key info
			keyInfo, err := prov.GetKey(key)
			if err != nil {
				WriteError(w, http.StatusForbidden,
					config.ErrCodeInvalidKey,
					config.DefaultErrors[config.ErrCodeInvalidKey])
				return
			}

			// Attach key info to context
			if soajsCtx != nil {
				soajsCtx.Key = keyInfo
			}

			next.ServeHTTP(w, r)
		})
	}
}
