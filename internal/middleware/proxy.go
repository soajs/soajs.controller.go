package middleware

import (
	"encoding/json"
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
	"go.uber.org/zap"
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
			servicePath := serviceParams.URLInfo.ServicePath

			// Handle /soajs/proxy - extract proxyRoute query param and route to that service
			if serviceName == "soajs" && serviceParams.URLInfo.ServiceVersion == "proxy" {
				proxyRoute := r.URL.Query().Get("proxyRoute")
				if proxyRoute == "" {
					WriteError(w, http.StatusBadRequest,
						config.ErrCodeInvalidRoute,
						"proxyRoute query parameter is required")
					return
				}

				// Decode the proxyRoute
				decodedRoute, err := url.QueryUnescape(proxyRoute)
				if err != nil {
					decodedRoute = proxyRoute
				}

				// Parse the proxyRoute to extract service name
				// Format: /serviceName/path or /serviceName/version/path
				parts := strings.Split(strings.TrimPrefix(decodedRoute, "/"), "/")
				if len(parts) < 1 || parts[0] == "" {
					WriteError(w, http.StatusBadRequest,
						config.ErrCodeInvalidRoute,
						"Invalid proxyRoute format")
					return
				}

				serviceName = parts[0]
				serviceVersion = "1" // default version
				servicePath = "/" + strings.Join(parts[1:], "/")

				// Check if second part is a version number
				if len(parts) > 1 {
					if _, err := fmt.Sscanf(parts[1], "%d", new(int)); err == nil {
						serviceVersion = parts[1]
						servicePath = "/" + strings.Join(parts[2:], "/")
					}
				}

				// Update serviceParams for downstream middleware
				serviceParams.URLInfo.ServiceName = serviceName
				serviceParams.URLInfo.ServiceVersion = serviceVersion
				serviceParams.URLInfo.ServicePath = servicePath
			}

			// Skip other /soajs/* routes - they're handled by SoajsRoutes middleware
			if serviceName == "soajs" {
				next.ServeHTTP(w, r)
				return
			}

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
	// Build the soajsinjectobj - this is the JSON object that SOAJS services expect
	injectObj := make(map[string]interface{})

	// Tenant info
	if soajsCtx.Tenant != nil {
		injectObj["tenant"] = map[string]interface{}{
			"id":     soajsCtx.Tenant.ID,
			"code":   soajsCtx.Tenant.Code,
			"name":   soajsCtx.Tenant.Name,
			"type":   soajsCtx.Tenant.Type,
			"locked": false,
		}
	}

	// Key info - config must be at least an empty object, never null
	if soajsCtx.Key != nil {
		keyConfig := soajsCtx.Key.Config
		if keyConfig == nil {
			keyConfig = make(map[string]interface{})
		}
		injectObj["key"] = map[string]interface{}{
			"iKey":   soajsCtx.Key.InternalKey, // internal key (32 chars)
			"eKey":   soajsCtx.Key.ExtKey,
			"config": keyConfig,
		}
	}

	// Application info (from key)
	if soajsCtx.Key != nil {
		appObj := map[string]interface{}{
			"product": soajsCtx.Key.ProductCode,
			"package": soajsCtx.Key.PackageCode,
			"appId":   soajsCtx.Key.AppID,
		}
		// Include application ACLs (can be null)
		if soajsCtx.Key.AppACL != nil {
			appObj["acl"] = soajsCtx.Key.AppACL
		} else {
			appObj["acl"] = nil
		}
		if soajsCtx.Key.AppACLAllEnv != nil {
			appObj["acl_all_env"] = soajsCtx.Key.AppACLAllEnv
		} else {
			appObj["acl_all_env"] = nil
		}
		injectObj["application"] = appObj
	}

	// Package info
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		pkg := soajsCtx.Controller.ServiceParams.Package
		if pkg != nil {
			pkgACL := pkg.ACL
			if pkgACL == nil {
				pkgACL = make(map[string]interface{})
			}
			injectObj["package"] = map[string]interface{}{
				"acl":         pkgACL,
				"acl_all_env": nil,
			}
		}
	}

	// Device and Geo info
	if soajsCtx.Device != nil {
		injectObj["device"] = soajsCtx.Device
	} else {
		injectObj["device"] = map[string]interface{}{}
	}
	if soajsCtx.Geo != nil {
		injectObj["geo"] = soajsCtx.Geo
	} else {
		injectObj["geo"] = map[string]interface{}{}
	}

	// Awareness info - controller host and port for service callbacks
	// Default to localhost:4000 (standard controller port)
	injectObj["awareness"] = map[string]interface{}{
		"host": "127.0.0.1",
		"port": 4000,
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(injectObj)
	if err != nil {
		soajsCtx.Log.Error("failed to serialize soajsinjectobj")
		return
	}

	// Debug: log the soajsinjectobj content
	soajsCtx.Log.Debug("soajsinjectobj header", zap.String("content", string(jsonBytes)))

	// Set the header
	req.Header.Set("soajsinjectobj", string(jsonBytes))
}

// stripPrefix removes a prefix from the path
func stripPrefix(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}
