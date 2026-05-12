package middleware

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
)

// ProxyConfig holds proxy configuration
type ProxyConfig struct {
	Timeout         time.Duration
	MaxIdleConns    int
	IdleConnTimeout time.Duration
}

// DefaultProxyConfig returns default proxy configuration
func DefaultProxyConfig() ProxyConfig {
	return ProxyConfig{
		Timeout:         30 * time.Second,
		MaxIdleConns:    100,
		IdleConnTimeout: 90 * time.Second,
	}
}

var proxyClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

// GotoService middleware determines the target service and sets up proxying
func GotoService(prov provision.Provision, reg registry.Registry, aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx == nil || soajsCtx.Controller == nil {
				next.ServeHTTP(w, r)
				return
			}

			serviceParams := soajsCtx.Controller.ServiceParams
			if serviceParams == nil || serviceParams.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}

			serviceName := serviceParams.URLInfo.ServiceName
			serviceVersion := serviceParams.URLInfo.ServiceVersion

			// Get service host from awareness
			hostInfo, err := aw.GetHost(serviceName, serviceVersion)
			if err != nil || hostInfo == nil {
				WriteError(w, http.StatusBadGateway,
					config.ErrCodeServiceNotFound,
					config.DefaultErrors[config.ErrCodeServiceNotFound])
				return
			}

			// Build target URL
			targetURL := buildTargetURL(hostInfo.Host, hostInfo.Port, serviceParams.URLInfo.ServicePath, r.URL.RawQuery)

			// Set the gotoservice function
			soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
				proxyRequest(w, r, targetURL, soajsCtx)
			}

			next.ServeHTTP(w, r)
		})
	}
}

func buildTargetURL(host string, port int, path string, query string) string {
	u := &url.URL{
		Scheme:   "http",
		Host:     fmt.Sprintf("%s:%d", host, port),
		Path:     path,
		RawQuery: query,
	}
	return u.String()
}

func proxyRequest(w http.ResponseWriter, r *http.Request, targetURL string, soajsCtx *SOAJSContext) {
	// Create proxy request
	proxyReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		soajsCtx.Log.Error("failed to create proxy request")
		WriteError(w, http.StatusBadGateway, config.ErrCodeServiceNotFound, "Failed to proxy request")
		return
	}

	// Copy headers
	copyHeaders(proxyReq.Header, r.Header)

	// Add/modify proxy headers
	proxyReq.Header.Set("X-Forwarded-For", getClientIP(r))
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-Proto", getScheme(r))

	// Inject SOAJS context if configured
	if soajsCtx.Tenant != nil {
		injectSOAJSHeaders(proxyReq, soajsCtx)
	}

	// Execute proxy request
	resp, err := proxyClient.Do(proxyReq)
	if err != nil {
		soajsCtx.Log.Error("proxy request failed")
		WriteError(w, http.StatusBadGateway, config.ErrCodeServiceNotFound, "Service unavailable")
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	copyHeaders(w.Header(), resp.Header)

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	io.Copy(w, resp.Body)
}

func copyHeaders(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func getScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if scheme := r.Header.Get("X-Forwarded-Proto"); scheme != "" {
		return scheme
	}
	return "http"
}

func injectSOAJSHeaders(req *http.Request, soajsCtx *SOAJSContext) {
	// Inject tenant/key info as header for downstream services
	if soajsCtx.Tenant != nil {
		req.Header.Set("X-SOAJS-Tenant-Code", soajsCtx.Tenant.Code)
		req.Header.Set("X-SOAJS-Tenant-ID", soajsCtx.Tenant.ID)
	}
	if soajsCtx.Key != nil {
		req.Header.Set("X-SOAJS-Key-ExtKey", soajsCtx.Key.ExtKey)
	}
}

// stripPrefix removes a prefix from the path
func stripPrefix(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}
