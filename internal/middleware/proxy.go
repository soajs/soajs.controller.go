package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	"github.com/soajs/soajs.controller.go/pkg/types"
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

			// Check for roaming header - if present and valid, handle roaming
			// This must happen before normal service routing
			if HandleRoaming(w, r, soajsCtx, reg) {
				// Roaming was handled (success or error)
				return
			}

			serviceName := serviceParams.URLInfo.ServiceName
			serviceVersion := serviceParams.URLInfo.ServiceVersion
			servicePath := serviceParams.URLInfo.ServicePath

			// Skip routing if no service name (e.g., root path "/")
			if serviceName == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Handle /soajs/proxy - extract proxyRoute query param and route to that service
			// Note: URL middleware parses /soajs/proxy as ServiceName="soajs", ServicePath="/proxy"
			if serviceName == "soajs" && serviceParams.URLInfo.ServicePath == "/proxy" {
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

			// Check if service requires external key (extKeyRequired)
			// If true and no key is provided, return error 132
			regData := reg.Get()
			if regData != nil && regData.Services != nil {
				if svc, ok := regData.Services[serviceName]; ok {
					// Check version-specific extKeyRequired
					extKeyRequired := false
					if svc.Versions != nil {
						// Get version to check - use specified version or find latest
						ver := serviceVersion
						if ver == "" {
							// Get latest version from service hosts or find from versions map
							if svc.Hosts != nil && svc.Hosts.Latest != "" {
								ver = svc.Hosts.Latest
							} else {
								// Find latest from versions map
								for v := range svc.Versions {
									if ver == "" || v > ver {
										ver = v
									}
								}
							}
						}
						if ver != "" {
							if vInfo, vok := svc.Versions[ver]; vok && vInfo != nil {
								extKeyRequired = vInfo.ExtKeyRequired
							}
						}
					}

					// If extKeyRequired, check for key
					if extKeyRequired {
						key := r.Header.Get("key")
						if key == "" {
							key = r.URL.Query().Get("key")
						}
						if key == "" {
							WriteError(w, http.StatusUnauthorized,
								config.ErrCodeKeyRequired,
								config.DefaultErrors[config.ErrCodeKeyRequired])
							return
						}
					}
				}
			}

			// Set the gotoservice function
			// Note: Host resolution is deferred to execution time so MT middleware can
			// resolve the correct version from finalACL when version wasn't in URL
			soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
				// Get the service version (may have been updated by MT middleware from finalACL)
				svcVersion := soajsCtx.Controller.ServiceParams.URLInfo.ServiceVersion

				// If version is still empty and not requested in URL, default to "1"
				// (This happens when finalACL didn't have version info)
				if svcVersion == "" && !soajsCtx.Controller.ServiceParams.URLInfo.VersionRequested {
					svcVersion = "1"
				}

				// Get service host from awareness
				hostInfo, err := aw.GetHost(serviceName, svcVersion)
				if err != nil || hostInfo == nil {
					WriteError(w, http.StatusBadGateway,
						config.ErrCodeServiceNotFound,
						config.DefaultErrors[config.ErrCodeServiceNotFound])
					return
				}

				// Build target URL
				targetURL := buildTargetURL(hostInfo.Host, hostInfo.Port, soajsCtx.Controller.ServiceParams.URLInfo.ServicePath, r.URL.RawQuery)
				proxyRequest(w, r, targetURL, soajsCtx, reg, aw)
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

func proxyRequest(w http.ResponseWriter, r *http.Request, targetURL string, soajsCtx *SOAJSContext, reg registry.Registry, aw awareness.Awareness) {
	// Get request timeout from service params (with default fallback)
	requestTimeout := 30 * time.Second
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		// Check if service has custom timeout
		// TODO: Read from registry.services[name].requestTimeout
	}

	// Create context with timeout for the request
	ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
	defer cancel()

	// Create proxy request with timeout context
	proxyReq, err := http.NewRequestWithContext(ctx, r.Method, targetURL, r.Body)
	if err != nil {
		soajsCtx.Log.Error("failed to create proxy request")
		WriteError(w, http.StatusBadGateway, config.ErrCodeServiceNotFound, "Failed to proxy request")
		return
	}

	// Copy headers
	copyHeaders(proxyReq.Header, r.Header)

	// Parse target URL to get hostname for Host header
	targetURLParsed, _ := url.Parse(targetURL)
	if targetURLParsed != nil && targetURLParsed.Host != "" {
		// Set Host header to match target hostname (matches Node.js behavior)
		// This is critical - the target service expects Host to match its hostname
		proxyReq.Host = targetURLParsed.Host
		proxyReq.Header.Set("Host", targetURLParsed.Host)
	}

	// Add/modify proxy headers
	proxyReq.Header.Set("X-Forwarded-For", getClientIP(r))
	proxyReq.Header.Set("X-Forwarded-Host", r.Host)
	proxyReq.Header.Set("X-Forwarded-Proto", getScheme(r))

	// Handle soajsauth header - convert to session cookie
	handleSoajsAuth(r, proxyReq, soajsCtx)

	// Inject SOAJS context if configured
	if soajsCtx.Tenant != nil {
		if err := injectSOAJSHeaders(proxyReq, soajsCtx, reg, aw); err != nil {
			soajsCtx.Log.Error("failed to inject SOAJS headers", zap.Error(err))
			WriteError(w, http.StatusRequestHeaderFieldsTooLarge, config.ErrCodeHeaderTooLarge,
				config.DefaultErrors[config.ErrCodeHeaderTooLarge])
			return
		}
	}

	soajsCtx.Log.Debug("proxy request starting", zap.String("targetURL", targetURL))

	resp, err := proxyClient.Do(proxyReq)
	if err != nil {
		soajsCtx.Log.Error("proxy request failed", zap.Error(err), zap.String("targetURL", targetURL))
		WriteError(w, http.StatusBadGateway, config.ErrCodeServiceNotFound, "Service unavailable")
		return
	}
	defer resp.Body.Close()

	soajsCtx.Log.Debug("proxy response received", zap.Int("statusCode", resp.StatusCode))

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

func injectSOAJSHeaders(req *http.Request, soajsCtx *SOAJSContext, reg registry.Registry, aw awareness.Awareness) error {
	// Build the soajsinjectobj - this is the JSON object that SOAJS services expect
	// Matches Node.js mw/mt/index.js behavior exactly
	injectObj := make(map[string]interface{})

	// Get service version config to determine what to include
	var serviceConfig *types.VersionInfo
	serviceName := ""
	serviceVersion := ""
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil && reg != nil {
		regData := reg.Get()
		if regData != nil && regData.Services != nil {
			if soajsCtx.Controller.ServiceParams.URLInfo != nil {
				serviceName = soajsCtx.Controller.ServiceParams.URLInfo.ServiceName
				serviceVersion = soajsCtx.Controller.ServiceParams.URLInfo.ServiceVersion
			}
			if svc, ok := regData.Services[serviceName]; ok && svc.Versions != nil {
				if vInfo, vok := svc.Versions[serviceVersion]; vok {
					serviceConfig = vInfo
				}
			}
		}
	}

	// Service parameter flags - all false unless service explicitly requests (Node.js line 58)
	provisionACL := false
	urac := false
	uracProfile := false
	uracACL := false
	uracConfig := false
	uracGroupConfig := false
	tenantProfile := false

	if serviceConfig != nil {
		provisionACL = serviceConfig.ProvisionACL
		urac = serviceConfig.Urac
		uracProfile = serviceConfig.UracProfile
		uracACL = serviceConfig.UracACL
		uracConfig = serviceConfig.UracConfig
		uracGroupConfig = serviceConfig.UracGroupConfig
		tenantProfile = serviceConfig.TenantProfile
	}

	// Tenant info (Node.js builds full tenant object)
	if soajsCtx.Tenant != nil {
		tenantObj := map[string]interface{}{
			"id":     soajsCtx.Tenant.ID,
			"code":   soajsCtx.Tenant.Code,
			"name":   soajsCtx.Tenant.Name,
			"type":   soajsCtx.Tenant.Type,
			"locked": soajsCtx.Tenant.Locked,
		}
		// Add roaming if present
		if soajsCtx.Tenant.Roaming != nil {
			tenantObj["roaming"] = soajsCtx.Tenant.Roaming
		}
		// Add main if present
		if soajsCtx.Tenant.Main != nil {
			tenantObj["main"] = soajsCtx.Tenant.Main
		}
		// Only include tenant profile if service requests it
		if tenantProfile && soajsCtx.Tenant.Profile != nil {
			tenantObj["profile"] = soajsCtx.Tenant.Profile
		}
		injectObj["tenant"] = tenantObj
	}

	// Key info - only include service-specific config, not full config
	if soajsCtx.Key != nil {
		// Build minimal config with only commonFields and service-specific config
		keyConfig := make(map[string]interface{})
		if soajsCtx.Key.Config != nil {
			if commonFields, exists := soajsCtx.Key.Config["commonFields"]; exists {
				keyConfig["commonFields"] = commonFields
			}
			if serviceName != "" {
				if svcConfig, exists := soajsCtx.Key.Config[serviceName]; exists {
					keyConfig[serviceName] = svcConfig
				}
			}
		}

		injectObj["key"] = map[string]interface{}{
			"iKey":   soajsCtx.Key.InternalKey,
			"eKey":   soajsCtx.Key.ExtKey,
			"config": keyConfig,
		}
	}

	// Application info - build with ACL initially (Node.js includes then deletes)
	if soajsCtx.Key != nil {
		appObj := map[string]interface{}{
			"product": soajsCtx.Key.ProductCode,
			"package": soajsCtx.Key.PackageCode,
			"appId":   soajsCtx.Key.AppID,
		}
		// Include ACL - will be removed below if not needed
		appObj["acl"] = soajsCtx.Key.AppACL
		appObj["acl_all_env"] = soajsCtx.Key.AppACLAllEnv
		injectObj["application"] = appObj
	}

	// Package info - build with ACL initially (Node.js includes then deletes)
	pkgObj := make(map[string]interface{})
	if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
		pkg := soajsCtx.Controller.ServiceParams.Package
		if pkg != nil {
			pkgObj["acl"] = pkg.ACL
			pkgObj["acl_all_env"] = pkg.ACLAllEnv
		}
	}
	injectObj["package"] = pkgObj

	// Device and Geo info - always included
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

	// Urac info - only if service requests it (Node.js line ~193)
	if urac && soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil && soajsCtx.Controller.ServiceParams.UserInfo != nil {
		userMap := soajsCtx.Controller.ServiceParams.UserInfo
		// Build urac object with base fields
		uracObj := map[string]interface{}{
			"_id":         userMap["_id"],
			"username":    userMap["username"],
			"firstName":   encodeHeaderValue(getString(userMap, "firstName")),
			"lastName":    encodeHeaderValue(getString(userMap, "lastName")),
			"email":       userMap["email"],
			"groups":      userMap["groups"],
			"socialLogin": userMap["socialLogin"],
		}
		if tenant, ok := userMap["tenant"].(map[string]interface{}); ok {
			uracObj["tenant"] = map[string]interface{}{
				"id":   tenant["id"],
				"code": tenant["code"],
			}
		}

		// Add param to indicate what was included
		injectObj["param"] = map[string]interface{}{
			"urac_Profile": uracProfile,
			"urac_ACL":     uracACL,
		}

		if uracProfile {
			uracObj["profile"] = userMap["profile"]
		}
		if uracACL {
			uracObj["acl"] = userMap["acl"]
			uracObj["acl_AllEnv"] = userMap["acl_AllEnv"]
		}
		if uracConfig {
			uracObj["config"] = userMap["config"]
		}
		if uracGroupConfig {
			uracObj["groupsConfig"] = userMap["groupsConfig"]
		}
		injectObj["urac"] = uracObj
	}

	// Delete ACL data if not requested (Node.js line ~286)
	if !provisionACL {
		if appObj, ok := injectObj["application"].(map[string]interface{}); ok {
			delete(appObj, "acl")
			delete(appObj, "acl_all_env")
		}
		if pkgObj, ok := injectObj["package"].(map[string]interface{}); ok {
			delete(pkgObj, "acl")
			delete(pkgObj, "acl_all_env")
		}
	}

	// Awareness info - controller host and port for service callbacks
	awarenessObj := map[string]interface{}{
		"host": "127.0.0.1",
		"port": 4000,
	}

	// Get controller port from registry if available
	if reg != nil {
		regData := reg.Get()
		if regData != nil && regData.ServiceConfig != nil {
			if regData.ServiceConfig.Ports.Controller > 0 {
				awarenessObj["port"] = regData.ServiceConfig.Ports.Controller
			}
		}
	}

	// Build interConnect array if service has interConnect configured
	if serviceConfig != nil && len(serviceConfig.InterConnect) > 0 && reg != nil && aw != nil {
		interConnectArr := make([]map[string]interface{}, 0)
		regData := reg.Get()

		for _, ic := range serviceConfig.InterConnect {
			if ic.Name == "" {
				continue
			}
			// Check if service exists in registry
			icSvc, icExists := regData.Services[ic.Name]
			if !icExists || icSvc.Port == 0 {
				soajsCtx.Log.Debug("interConnect service not found in registry",
					zap.String("service", ic.Name))
				continue
			}

			// Add service config for interConnect service if available
			if soajsCtx.Key != nil && soajsCtx.Key.Config != nil {
				if keyObj, ok := injectObj["key"].(map[string]interface{}); ok {
					if keyConfig, ok := keyObj["config"].(map[string]interface{}); ok {
						if icConfig, exists := soajsCtx.Key.Config[ic.Name]; exists {
							keyConfig[ic.Name] = icConfig
						}
					}
				}
			}

			// Get host from awareness
			icVersion := ic.Version
			hostInfo, err := aw.GetHost(ic.Name, icVersion)
			if err != nil || hostInfo == nil {
				soajsCtx.Log.Debug("interConnect failed to get host",
					zap.String("service", ic.Name),
					zap.String("version", icVersion))
				continue
			}

			// Get latest version
			latestVersion := ""
			if icSvc.Hosts != nil && icSvc.Hosts.Latest != "" {
				latestVersion = icSvc.Hosts.Latest
			} else if icSvc.Versions != nil {
				for v := range icSvc.Versions {
					if latestVersion == "" || v > latestVersion {
						latestVersion = v
					}
				}
			}

			icItem := map[string]interface{}{
				"name":    ic.Name,
				"version": hostInfo.Version,
				"host":    hostInfo.Host,
				"port":    icSvc.Port,
				"latest":  latestVersion,
			}
			interConnectArr = append(interConnectArr, icItem)
		}

		if len(interConnectArr) > 0 {
			awarenessObj["interConnect"] = interConnectArr
		}
	}

	injectObj["awareness"] = awarenessObj

	// Serialize to JSON
	jsonBytes, err := json.Marshal(injectObj)
	if err != nil {
		soajsCtx.Log.Error("failed to serialize soajsinjectobj")
		return fmt.Errorf("failed to serialize soajsinjectobj: %w", err)
	}

	// Security: Validate and limit soajsinjectobj header size (Node.js line ~318)
	maxHeaderSize := 65536 // 64KB default
	if len(jsonBytes) > maxHeaderSize {
		soajsCtx.Log.Error("soajsinjectobj header exceeds maximum size",
			zap.Int("size", len(jsonBytes)),
			zap.Int("maxSize", maxHeaderSize))
		return fmt.Errorf("soajsinjectobj header exceeds maximum size: %d > %d", len(jsonBytes), maxHeaderSize)
	}

	// Log the soajsinjectobj content (DEBUG level)
	soajsCtx.Log.Debug("soajsinjectobj header",
		zap.Int("size", len(jsonBytes)),
		zap.String("content", string(jsonBytes)))

	// Set the header
	req.Header.Set("soajsinjectobj", string(jsonBytes))
	return nil
}

// encodeHeaderValue encodes a string value for safe HTTP header inclusion
func encodeHeaderValue(val string) string {
	// URL encode the value to handle special characters
	return url.QueryEscape(val)
}

// getString safely extracts a string from a map
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// stripPrefix removes a prefix from the path
func stripPrefix(path, prefix string) string {
	return strings.TrimPrefix(path, prefix)
}

// handleSoajsAuth checks for soajsauth header/query and converts to session cookie
// Note: Session config should be obtained from registry.serviceConfig.session
// Currently uses defaults - will be enhanced when registry is available in context
func handleSoajsAuth(r, proxyReq *http.Request, soajsCtx *SOAJSContext) {
	// Check for soajsauth header
	soajsauth := r.Header.Get("soajsauth")
	if soajsauth == "" {
		// Check query params
		soajsauth = r.URL.Query().Get("soajsauth")
	}

	if soajsauth == "" {
		return
	}

	// TODO: Get session config from registry.serviceConfig.session
	// For now, use ServicesConfig if available
	sessionSecret := ""
	sessionName := "soajs.sid"

	if soajsCtx.ServicesConfig != nil {
		if session, ok := soajsCtx.ServicesConfig["session"].(map[string]interface{}); ok {
			if secret, ok := session["secret"].(string); ok && secret != "" {
				sessionSecret = secret
			}
			if name, ok := session["name"].(string); ok && name != "" {
				sessionName = name
			}
		}
	}

	// If no session secret, we can't create the cookie
	if sessionSecret == "" {
		if soajsCtx.Log != nil {
			soajsCtx.Log.Debug("soajsauth found but no session secret configured")
		}
		return
	}

	// Convert soajsauth to session cookie
	cookie := setSessionCookie(soajsauth, sessionSecret, sessionName)
	if cookie == "" {
		return
	}

	// Get existing cookies and append
	cookies := proxyReq.Header.Get("Cookie")
	if cookies != "" {
		cookies = cookies + "; " + cookie
	} else {
		cookies = cookie
	}
	proxyReq.Header.Set("Cookie", cookies)
}

// setSessionCookie converts soajsauth to a signed session cookie
// soajsauth format: "Basic BASE64(soajs:MASKED_SESSION_ID)"
func setSessionCookie(auth, secret, cookieName string) string {
	// Decode Base64 - format is "Basic BASE64(...)"
	auth = strings.TrimSpace(auth)
	parts := strings.Split(auth, "Basic ")
	if len(parts) < 2 {
		return ""
	}

	decoded, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return ""
	}

	// Format is "soajs:MASKED_SESSION_ID"
	ascii := string(decoded)
	if !strings.HasPrefix(ascii, "soajs:") {
		return ""
	}

	masked := strings.TrimPrefix(ascii, "soajs:")

	// Unmask the session ID
	sessionID := umaskSessionID(masked)
	if sessionID == "" {
		return ""
	}

	// Sign the session ID with HMAC-SHA256
	signed := "s:" + signCookie(sessionID, secret)

	return cookieName + "=" + signed
}

// umaskSessionID decyphers the session ID using the cypher code prefix
func umaskSessionID(masked string) string {
	if len(masked) < 3 {
		return ""
	}

	styleCode := masked[0:3]
	sid := masked[3:]

	if len(sid) != 32 {
		return sid // Return as-is if not expected length
	}

	// Apply decypher based on style code
	switch styleCode {
	case "C01":
		// Swap positions: 0-2, 11, 4-6, 15, 8-10, 3, 12-14, 7, 16+
		return sid[0:3] + string(sid[11]) + sid[4:7] + string(sid[15]) + sid[8:11] + string(sid[3]) + sid[12:15] + string(sid[7]) + sid[16:]
	case "C02":
		return sid[0:4] + string(sid[12]) + sid[5:9] + string(sid[15]) + sid[10:12] + string(sid[4]) + sid[13:15] + string(sid[9]) + sid[16:]
	case "C03":
		return sid[0:5] + string(sid[13]) + sid[6:11] + string(sid[15]) + sid[12:13] + string(sid[5]) + sid[14:15] + string(sid[11]) + sid[16:]
	default:
		return sid
	}
}

// signCookie creates HMAC-SHA256 signature for session cookie
func signCookie(val, secret string) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(val))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))
	// Remove trailing = signs like Node.js does
	signature = strings.TrimRight(signature, "=")
	return val + "." + signature
}
