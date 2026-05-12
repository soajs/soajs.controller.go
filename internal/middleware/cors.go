package middleware

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/registry"
)

// CORSConfig holds CORS configuration
type CORSConfig struct {
	Enabled          bool
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int
}

// DefaultCORSConfig returns the default CORS configuration
func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "HEAD", "PUT", "PATCH", "POST", "DELETE"},
		AllowedHeaders:   []string{"__env", "key", "soajsauth", "Keep-Alive", "User-Agent", "X-Requested-With", "If-Modified-Since", "Cache-Control", "Content-Type", "Authorization", "access_token", "soajsinjectobj"},
		ExposedHeaders:   []string{"__env", "key", "soajsauth", "Keep-Alive", "User-Agent", "X-Requested-With", "If-Modified-Since", "Cache-Control", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           1728000,
	}
}

// CORS middleware handles Cross-Origin Resource Sharing
func CORS() Middleware {
	cfg := DefaultCORSConfig()
	return CORSWithConfig(cfg)
}

// CORSWithRegistry creates CORS middleware that reads config from registry
func CORSWithRegistry(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cfg := DefaultCORSConfig()

			// Try to get CORS config from registry
			regData := reg.Get()
			if regData != nil && regData.ServiceConfig != nil {
				// Registry CORS config would be applied here if available
				// For now, use defaults
			}

			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}

			// Handle preflight request
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", fmt.Sprintf("%t", cfg.AllowCredentials))
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ","))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ","))
				w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ","))
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Set CORS headers for non-preflight requests
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", fmt.Sprintf("%t", cfg.AllowCredentials))
			w.Header().Set("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ","))

			next.ServeHTTP(w, r)
		})
	}
}

// CORSWithConfig creates CORS middleware with custom config
func CORSWithConfig(cfg CORSConfig) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = "*"
			}

			// Handle preflight request
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", fmt.Sprintf("%t", cfg.AllowCredentials))
				w.Header().Set("Access-Control-Allow-Methods", strings.Join(cfg.AllowedMethods, ","))
				w.Header().Set("Access-Control-Allow-Headers", strings.Join(cfg.AllowedHeaders, ","))
				w.Header().Set("Access-Control-Max-Age", fmt.Sprintf("%d", cfg.MaxAge))
				w.Header().Set("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ","))
				w.WriteHeader(http.StatusNoContent)
				return
			}

			// Set CORS headers for non-preflight requests
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", fmt.Sprintf("%t", cfg.AllowCredentials))
			w.Header().Set("Access-Control-Expose-Headers", strings.Join(cfg.ExposedHeaders, ","))

			next.ServeHTTP(w, r)
		})
	}
}
