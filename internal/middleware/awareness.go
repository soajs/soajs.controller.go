package middleware

import (
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/awareness"
)

// Awareness middleware injects service discovery capabilities
func Awareness(aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx != nil {
				soajsCtx.Awareness = &awarenessClientWrapper{aw: aw}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// AwarenessEnv middleware injects environment-aware service discovery
func AwarenessEnv(aw awareness.Awareness, currentEnv string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx != nil {
				soajsCtx.AwarenessEnv = &awarenessEnvClientWrapper{
					aw:         aw,
					currentEnv: currentEnv,
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// awarenessClientWrapper wraps awareness for the middleware interface
type awarenessClientWrapper struct {
	aw awareness.Awareness
}

func (w *awarenessClientWrapper) GetHost(serviceName string, version string) (string, error) {
	hostInfo, err := w.aw.GetHost(serviceName, version)
	if err != nil {
		return "", err
	}
	return hostInfo.Host, nil
}

// awarenessEnvClientWrapper wraps awareness for environment-aware lookups
type awarenessEnvClientWrapper struct {
	aw         awareness.Awareness
	currentEnv string
}

func (w *awarenessEnvClientWrapper) GetHost(env string, serviceName string, version string) (string, error) {
	// For now, only support current environment
	// Cross-environment awareness would require additional infrastructure
	if env != w.currentEnv {
		return "", nil
	}
	hostInfo, err := w.aw.GetHost(serviceName, version)
	if err != nil {
		return "", err
	}
	return hostInfo.Host, nil
}
