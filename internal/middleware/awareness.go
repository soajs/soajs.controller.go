package middleware

import (
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/awareness"
)

// Awareness gives the request the hosts of the environment's services (req.soajs.awareness).
func Awareness(aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if soajsCtx := GetSOAJSContext(r.Context()); soajsCtx != nil {
				soajsCtx.Awareness = awarenessClient{aw}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AwarenessEnv gives the request the hosts of the gateway in other environments
// (req.soajs.awarenessEnv).
func AwarenessEnv(aw awareness.EnvAwareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if soajsCtx := GetSOAJSContext(r.Context()); soajsCtx != nil {
				soajsCtx.AwarenessEnv = awarenessEnvClient{aw}
			}
			next.ServeHTTP(w, r)
		})
	}
}

type awarenessClient struct{ aw awareness.Awareness }

// GetHost returns the host of a service version, "" with awareness.ErrNoHost when there is none.
func (c awarenessClient) GetHost(serviceName, version string) (string, error) {
	info, err := c.aw.GetHost(serviceName, version)
	if err != nil {
		return "", err
	}
	return info.Host, nil
}

type awarenessEnvClient struct{ aw awareness.EnvAwareness }

// GetHost returns the host of the gateway in env; the service is always the gateway.
func (c awarenessEnvClient) GetHost(env, _ string, version string) (string, error) {
	if host := c.aw.GetHost(env, version); host != "" {
		return host, nil
	}
	return "", awareness.ErrNoHost
}
