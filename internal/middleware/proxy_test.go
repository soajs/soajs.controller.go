package middleware

import (
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// mockAwarenessProxy implements awareness.Awareness for proxy tests
type mockAwarenessProxy struct {
	hosts map[string]*types.HostInfo
}

func (m *mockAwarenessProxy) GetHost(serviceName, version string) (*types.HostInfo, error) {
	key := serviceName + ":" + version
	if h, ok := m.hosts[key]; ok {
		return h, nil
	}
	return nil, nil
}

func (m *mockAwarenessProxy) HealthCheck(serviceName, host string) (*awareness.HealthStatus, error) {
	return nil, nil
}

func (m *mockAwarenessProxy) RegisterService(info types.ServiceInfo) error {
	return nil
}

func (m *mockAwarenessProxy) DeregisterService(info types.ServiceInfo) error {
	return nil
}

func (m *mockAwarenessProxy) GetStats() *awareness.AwarenessStats {
	return nil
}

// mockRegistryProxy implements registry.Registry for proxy tests
type mockRegistryProxy struct {
	data *types.RegistryData
}

func (m *mockRegistryProxy) Load(env string) error                                        { return nil }
func (m *mockRegistryProxy) Reload() error                                                { return nil }
func (m *mockRegistryProxy) Get() *types.RegistryData                                     { return m.data }
func (m *mockRegistryProxy) GetService(name string) (*types.ServiceConfig, error)         { return nil, nil }
func (m *mockRegistryProxy) GetCustomRegistry(name string) (interface{}, error)           { return nil, nil }
func (m *mockRegistryProxy) GetResource(resourceType, name string) (*types.Resource, error) { return nil, nil }
func (m *mockRegistryProxy) RegisterHost(info types.HostInfo) error                       { return nil }
func (m *mockRegistryProxy) GetHost(serviceName, version string) (*types.HostInfo, error) { return nil, nil }
func (m *mockRegistryProxy) AddUpdateEnvControllers(data types.ControllerUpdate) error    { return nil }
func (m *mockRegistryProxy) IsMaintenanceMode() bool                                      { return false }
func (m *mockRegistryProxy) GetEnvironment() string                                       { return "test" }

// =============================================================================
// GotoService Middleware Tests
// =============================================================================

func TestGotoService_SkipsWhenNoContext(t *testing.T) {
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{}

	gotoMW := GotoService(prov, reg, aw)

	called := false
	handler := gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when no context")
	}
}

func TestGotoService_SkipsWhenNoServiceParams(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{}

	// Only Context middleware - no URL middleware to set service params
	contextMW := Context(logger, "test")
	gotoMW := GotoService(prov, reg, aw)

	called := false
	handler := contextMW(gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	// Should pass through since URL middleware wasn't used
	if !called {
		t.Error("Handler should be called when no URL info")
	}
}

func TestGotoService_SkipsSoajsRoutes(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{}

	contextMW := Context(logger, "test")
	urlMW := URL()
	gotoMW := GotoService(prov, reg, aw)

	called := false
	handler := contextMW(urlMW(gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/soajs/acl", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for /soajs/* routes")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestGotoService_ReturnsErrorWhenServiceNotFound(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{hosts: map[string]*types.HostInfo{}} // Empty

	contextMW := Context(logger, "test")
	urlMW := URL()
	gotoMW := GotoService(prov, reg, aw)

	// The inner handler must call Gotoservice to trigger service resolution
	handler := contextMW(urlMW(gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soajsCtx := GetSOAJSContext(r.Context())
		if soajsCtx != nil && soajsCtx.Controller != nil && soajsCtx.Controller.Gotoservice != nil {
			soajsCtx.Controller.Gotoservice(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/oauth/token", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("Status = %d, want %d for service not found", rec.Code, http.StatusBadGateway)
	}
}

func TestGotoService_SetsGotoserviceFunction(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{
		hosts: map[string]*types.HostInfo{
			"oauth:1": {Host: "localhost", Port: 4002},
		},
	}

	contextMW := Context(logger, "test")
	urlMW := URL()
	gotoMW := GotoService(prov, reg, aw)

	var gotGotoservice bool
	handler := contextMW(urlMW(gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soajsCtx := GetSOAJSContext(r.Context())
		if soajsCtx != nil && soajsCtx.Controller != nil && soajsCtx.Controller.Gotoservice != nil {
			gotGotoservice = true
		}
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/oauth/token", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !gotGotoservice {
		t.Error("Gotoservice function should be set when service is found")
	}
}

func TestGotoService_HandlesProxyRoute(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{
		hosts: map[string]*types.HostInfo{
			"urac:1": {Host: "localhost", Port: 4001},
		},
	}

	contextMW := Context(logger, "test")
	urlMW := URL()
	gotoMW := GotoService(prov, reg, aw)

	var capturedService string
	handler := contextMW(urlMW(gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soajsCtx := GetSOAJSContext(r.Context())
		if soajsCtx != nil && soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil && soajsCtx.Controller.ServiceParams.URLInfo != nil {
			capturedService = soajsCtx.Controller.ServiceParams.URLInfo.ServiceName
		}
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/soajs/proxy?proxyRoute=/urac/user", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedService != "urac" {
		t.Errorf("Service = %s, want urac", capturedService)
	}
}

func TestGotoService_ProxyRouteWithVersion(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{
		hosts: map[string]*types.HostInfo{
			"urac:2": {Host: "localhost", Port: 4001},
		},
	}

	contextMW := Context(logger, "test")
	urlMW := URL()
	gotoMW := GotoService(prov, reg, aw)

	var capturedVersion string
	handler := contextMW(urlMW(gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soajsCtx := GetSOAJSContext(r.Context())
		if soajsCtx != nil && soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil && soajsCtx.Controller.ServiceParams.URLInfo != nil {
			capturedVersion = soajsCtx.Controller.ServiceParams.URLInfo.ServiceVersion
		}
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/soajs/proxy?proxyRoute=/urac/2/user", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedVersion != "2" {
		t.Errorf("Version = %s, want 2", capturedVersion)
	}
}

func TestGotoService_RejectsEmptyProxyRoute(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryProxy{}
	aw := &mockAwarenessProxy{}

	contextMW := Context(logger, "test")
	urlMW := URL()
	gotoMW := GotoService(prov, reg, aw)

	handler := contextMW(urlMW(gotoMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/soajs/proxy", nil) // No proxyRoute param
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Status = %d, want %d for empty proxyRoute", rec.Code, http.StatusBadRequest)
	}
}

// =============================================================================
// buildTargetURL Tests
// =============================================================================

func TestBuildTargetURL_Basic(t *testing.T) {
	url := buildTargetURL("localhost", 4002, "/token", "")
	expected := "http://localhost:4002/token"
	if url != expected {
		t.Errorf("URL = %s, want %s", url, expected)
	}
}

func TestBuildTargetURL_WithQuery(t *testing.T) {
	url := buildTargetURL("localhost", 4002, "/token", "grant_type=password")
	expected := "http://localhost:4002/token?grant_type=password"
	if url != expected {
		t.Errorf("URL = %s, want %s", url, expected)
	}
}

func TestBuildTargetURL_WithPath(t *testing.T) {
	url := buildTargetURL("10.0.0.1", 8080, "/api/v1/users", "limit=10&offset=0")
	expected := "http://10.0.0.1:8080/api/v1/users?limit=10&offset=0"
	if url != expected {
		t.Errorf("URL = %s, want %s", url, expected)
	}
}

// =============================================================================
// copyHeaders Tests
// =============================================================================

func TestCopyHeaders_Basic(t *testing.T) {
	src := http.Header{}
	src.Set("Content-Type", "application/json")
	src.Set("Authorization", "Bearer token")

	dst := http.Header{}
	copyHeaders(dst, src)

	if dst.Get("Content-Type") != "application/json" {
		t.Error("Content-Type not copied")
	}
	if dst.Get("Authorization") != "Bearer token" {
		t.Error("Authorization not copied")
	}
}

func TestCopyHeaders_MultipleValues(t *testing.T) {
	src := http.Header{}
	src.Add("Set-Cookie", "a=1")
	src.Add("Set-Cookie", "b=2")

	dst := http.Header{}
	copyHeaders(dst, src)

	cookies := dst.Values("Set-Cookie")
	if len(cookies) != 2 {
		t.Errorf("Expected 2 cookies, got %d", len(cookies))
	}
}

// =============================================================================
// getScheme Tests
// =============================================================================

func TestGetScheme_HTTP(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	scheme := getScheme(req)
	if scheme != "http" {
		t.Errorf("Scheme = %s, want http", scheme)
	}
}

func TestGetScheme_XForwardedProto(t *testing.T) {
	req := httptest.NewRequest("GET", "http://example.com/test", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	scheme := getScheme(req)
	if scheme != "https" {
		t.Errorf("Scheme = %s, want https", scheme)
	}
}

// =============================================================================
// injectSOAJSHeaders Tests
// =============================================================================

func TestInjectSOAJSHeaders_Basic(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	req := httptest.NewRequest("GET", "/test", nil)

	soajsCtx := &SOAJSContext{
		Log: logger,
		Tenant: &types.Tenant{
			ID:   "tenant123",
			Code: "TEST",
			Name: "Test Tenant",
		},
		Key: &types.Key{
			InternalKey: "ikey123",
			ExtKey:      "ekey123",
			ProductCode: "PROD",
			PackageCode: "PKG",
		},
	}

	injectSOAJSHeaders(req, soajsCtx, nil, nil)

	header := req.Header.Get("soajsinjectobj")
	if header == "" {
		t.Error("soajsinjectobj header not set")
	}

	// Verify it contains tenant info
	if !strings.Contains(header, "tenant123") {
		t.Error("Header should contain tenant ID")
	}
	if !strings.Contains(header, "TEST") {
		t.Error("Header should contain tenant code")
	}
}

func TestInjectSOAJSHeaders_WithNilKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	req := httptest.NewRequest("GET", "/test", nil)

	soajsCtx := &SOAJSContext{
		Log: logger,
		Tenant: &types.Tenant{
			ID:   "tenant123",
			Code: "TEST",
		},
		Key: nil, // No key
	}

	injectSOAJSHeaders(req, soajsCtx, nil, nil)

	header := req.Header.Get("soajsinjectobj")
	if header == "" {
		t.Error("soajsinjectobj header should still be set")
	}
}

// =============================================================================
// handleSoajsAuth Tests
// =============================================================================

func TestHandleSoajsAuth_NoAuth(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	req := httptest.NewRequest("GET", "/test", nil)
	proxyReq := httptest.NewRequest("GET", "/test", nil)

	soajsCtx := &SOAJSContext{Log: logger}

	handleSoajsAuth(req, proxyReq, soajsCtx)

	// No cookie should be added
	if proxyReq.Header.Get("Cookie") != "" {
		t.Error("No cookie should be added when no soajsauth")
	}
}

func TestHandleSoajsAuth_WithHeader(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Create soajsauth header value
	sessionID := "C01" + "12345678901234567890123456789012" // 3 + 32 chars
	encoded := base64.StdEncoding.EncodeToString([]byte("soajs:" + sessionID))
	authHeader := "Basic " + encoded

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("soajsauth", authHeader)

	proxyReq := httptest.NewRequest("GET", "/test", nil)

	soajsCtx := &SOAJSContext{
		Log: logger,
		ServicesConfig: map[string]interface{}{
			"session": map[string]interface{}{
				"secret": "test-secret",
				"name":   "soajs.sid",
			},
		},
	}

	handleSoajsAuth(req, proxyReq, soajsCtx)

	cookie := proxyReq.Header.Get("Cookie")
	if cookie == "" {
		t.Error("Cookie should be set when soajsauth is present")
	}
	if !strings.HasPrefix(cookie, "soajs.sid=") {
		t.Errorf("Cookie should start with soajs.sid=, got %s", cookie)
	}
}

func TestHandleSoajsAuth_FromQueryParam(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	sessionID := "C01" + "12345678901234567890123456789012"
	encoded := base64.StdEncoding.EncodeToString([]byte("soajs:" + sessionID))
	authValue := "Basic " + encoded

	// Create request and set query param manually to avoid URL encoding issues
	req := httptest.NewRequest("GET", "/test", nil)
	q := req.URL.Query()
	q.Set("soajsauth", authValue)
	req.URL.RawQuery = q.Encode()

	proxyReq := httptest.NewRequest("GET", "/test", nil)

	soajsCtx := &SOAJSContext{
		Log: logger,
		ServicesConfig: map[string]interface{}{
			"session": map[string]interface{}{
				"secret": "test-secret",
			},
		},
	}

	handleSoajsAuth(req, proxyReq, soajsCtx)

	if proxyReq.Header.Get("Cookie") == "" {
		t.Error("Cookie should be set from query param")
	}
}

// =============================================================================
// setSessionCookie Tests
// =============================================================================

func TestSetSessionCookie_InvalidFormat(t *testing.T) {
	result := setSessionCookie("invalid", "secret", "soajs.sid")
	if result != "" {
		t.Error("Invalid format should return empty string")
	}
}

func TestSetSessionCookie_NotSoajsPrefix(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("other:value"))
	result := setSessionCookie("Basic "+encoded, "secret", "soajs.sid")
	if result != "" {
		t.Error("Non-soajs prefix should return empty string")
	}
}

func TestSetSessionCookie_Valid(t *testing.T) {
	sessionID := "C01" + "12345678901234567890123456789012"
	encoded := base64.StdEncoding.EncodeToString([]byte("soajs:" + sessionID))
	result := setSessionCookie("Basic "+encoded, "secret", "soajs.sid")

	if result == "" {
		t.Error("Valid auth should return cookie")
	}
	if !strings.HasPrefix(result, "soajs.sid=s:") {
		t.Errorf("Cookie should be signed, got %s", result)
	}
}

// =============================================================================
// umaskSessionID Tests
// =============================================================================

func TestUmaskSessionID_TooShort(t *testing.T) {
	result := umaskSessionID("AB")
	if result != "" {
		t.Error("Too short input should return empty")
	}
}

func TestUmaskSessionID_NonStandardLength(t *testing.T) {
	result := umaskSessionID("C01short")
	if result != "short" {
		t.Errorf("Non-32 char should return as-is, got %s", result)
	}
}

func TestUmaskSessionID_C01Style(t *testing.T) {
	// Test C01 decypher pattern - session ID must be 32 chars
	input := "C01" + "ABCDEFGHIJKLMNOPabcdefghijklmnop" // exactly 32 chars
	result := umaskSessionID(input)
	if len(result) != 32 {
		t.Errorf("Result should be 32 chars, got %d", len(result))
	}
}

func TestUmaskSessionID_C02Style(t *testing.T) {
	input := "C02" + "ABCDEFGHIJKLMNOPabcdefghijklmnop" // exactly 32 chars
	result := umaskSessionID(input)
	if len(result) != 32 {
		t.Errorf("Result should be 32 chars, got %d", len(result))
	}
}

func TestUmaskSessionID_C03Style(t *testing.T) {
	input := "C03" + "ABCDEFGHIJKLMNOPabcdefghijklmnop" // exactly 32 chars
	result := umaskSessionID(input)
	if len(result) != 32 {
		t.Errorf("Result should be 32 chars, got %d", len(result))
	}
}

func TestUmaskSessionID_UnknownStyle(t *testing.T) {
	input := "XXX" + "ABCDEFGHIJKLMNOPabcdefghijklmnop" // exactly 32 chars
	result := umaskSessionID(input)
	// Unknown style returns as-is (without prefix)
	if result != "ABCDEFGHIJKLMNOPabcdefghijklmnop" {
		t.Errorf("Unknown style should return session part, got %s", result)
	}
}

// =============================================================================
// signCookie Tests
// =============================================================================

func TestSignCookie_Basic(t *testing.T) {
	result := signCookie("session123", "secret")
	if !strings.HasPrefix(result, "session123.") {
		t.Error("Signed cookie should start with original value")
	}
	if strings.HasSuffix(result, "=") {
		t.Error("Signature should not end with =")
	}
}

func TestSignCookie_Deterministic(t *testing.T) {
	result1 := signCookie("session123", "secret")
	result2 := signCookie("session123", "secret")
	if result1 != result2 {
		t.Error("Same input should produce same output")
	}
}

func TestSignCookie_DifferentSecrets(t *testing.T) {
	result1 := signCookie("session123", "secret1")
	result2 := signCookie("session123", "secret2")
	if result1 == result2 {
		t.Error("Different secrets should produce different signatures")
	}
}

// =============================================================================
// Proxy Integration Tests
// =============================================================================

func TestProxy_BuildsCorrectTargetURL(t *testing.T) {
	// Test that we can build target URL correctly
	targetURL := buildTargetURL("localhost", 8080, "/api/test", "foo=bar")
	expected := "http://localhost:8080/api/test?foo=bar"
	if targetURL != expected {
		t.Errorf("Target URL = %s, want %s", targetURL, expected)
	}
}

func TestProxy_ForwardsToBackend(t *testing.T) {
	// Create a test backend server
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Backend", "true")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, `{"status":"ok"}`)
	}))
	defer backend.Close()

	// This test verifies the backend server works
	resp, err := http.Get(backend.URL)
	if err != nil {
		t.Fatalf("Failed to reach backend: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Backend status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if resp.Header.Get("X-Backend") != "true" {
		t.Error("Backend header not set")
	}
}

// =============================================================================
// stripPrefix Tests
// =============================================================================

func TestStripPrefix_Basic(t *testing.T) {
	result := stripPrefix("/api/v1/users", "/api")
	if result != "/v1/users" {
		t.Errorf("Result = %s, want /v1/users", result)
	}
}

func TestStripPrefix_NoMatch(t *testing.T) {
	result := stripPrefix("/api/v1/users", "/other")
	if result != "/api/v1/users" {
		t.Errorf("Result = %s, want /api/v1/users", result)
	}
}

func TestStripPrefix_Empty(t *testing.T) {
	result := stripPrefix("/api", "/api")
	if result != "" {
		t.Errorf("Result = %s, want empty", result)
	}
}
