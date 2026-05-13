package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// mockRegistryMT implements registry.Registry for MT tests
type mockRegistryMT struct {
	data *types.RegistryData
}

func (m *mockRegistryMT) Load(env string) error                                        { return nil }
func (m *mockRegistryMT) Reload() error                                                { return nil }
func (m *mockRegistryMT) Get() *types.RegistryData                                     { return m.data }
func (m *mockRegistryMT) GetService(name string) (*types.ServiceConfig, error)         { return nil, nil }
func (m *mockRegistryMT) GetCustomRegistry(name string) (interface{}, error)           { return nil, nil }
func (m *mockRegistryMT) GetResource(resourceType, name string) (*types.Resource, error) { return nil, nil }
func (m *mockRegistryMT) RegisterHost(info types.HostInfo) error                       { return nil }
func (m *mockRegistryMT) GetHost(serviceName, version string) (*types.HostInfo, error) { return nil, nil }
func (m *mockRegistryMT) AddUpdateEnvControllers(data types.ControllerUpdate) error    { return nil }
func (m *mockRegistryMT) IsMaintenanceMode() bool                                      { return false }
func (m *mockRegistryMT) GetEnvironment() string                                       { return "test" }

// =============================================================================
// MT Middleware Tests
// =============================================================================

func TestMT_SkipsWhenNoKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryMT{}

	contextMW := Context(logger, "test")
	mtMW := MT(prov, reg, nil, "test")

	called := false
	handler := contextMW(mtMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	// No key header - should skip MT processing
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when no key present")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMT_RejectsInvalidTenant(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	// Key exists but tenant doesn't
	prov := &mockProvision{
		keys: map[string]*types.Key{
			"test-key": {
				ExtKey:     "test-key",
				TenantCode: "INVALID_TENANT",
			},
		},
		tenants: map[string]*types.Tenant{}, // Empty - no tenants
	}
	reg := &mockRegistryMT{}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)
	mtMW := MT(prov, reg, nil, "test")

	handler := contextMW(keyMW(mtMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "test-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d for invalid tenant", rec.Code, http.StatusForbidden)
	}
}

func TestMT_RejectsTenantInMaintenanceMode(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{
			"maint-key": {
				ExtKey:     "maint-key",
				TenantCode: "MAINT",
			},
		},
		tenants: map[string]*types.Tenant{
			"MAINT": {Code: "MAINT", MaintenanceMode: true},
		},
	}
	reg := &mockRegistryMT{}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)
	mtMW := MT(prov, reg, nil, "test")

	handler := contextMW(keyMW(mtMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "maint-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Status = %d, want %d for maintenance mode", rec.Code, http.StatusServiceUnavailable)
	}
}

func TestMT_RejectsInvalidProduct(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{
			"prod-key": {
				ExtKey:      "prod-key",
				TenantCode:  "TEST",
				ProductCode: "INVALID_PRODUCT",
			},
		},
		tenants: map[string]*types.Tenant{
			"TEST": {Code: "TEST", MaintenanceMode: false},
		},
		products: map[string]*types.Product{}, // Empty - no products
	}
	reg := &mockRegistryMT{}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)
	mtMW := MT(prov, reg, nil, "test")

	handler := contextMW(keyMW(mtMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "prod-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d for invalid product", rec.Code, http.StatusForbidden)
	}
}

func TestMT_RejectsInvalidPackage(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{
			"pkg-key": {
				ExtKey:      "pkg-key",
				TenantCode:  "TEST",
				ProductCode: "PROD",
				PackageCode: "INVALID_PKG",
			},
		},
		tenants: map[string]*types.Tenant{
			"TEST": {Code: "TEST"},
		},
		products: map[string]*types.Product{
			"PROD": {Code: "PROD"},
		},
		packages: map[string]*types.Package{}, // Empty - no packages
	}
	reg := &mockRegistryMT{}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)
	mtMW := MT(prov, reg, nil, "test")

	handler := contextMW(keyMW(mtMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "pkg-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d for invalid package", rec.Code, http.StatusForbidden)
	}
}

func TestMT_AllowsValidTenantProductPackage(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{
			"valid-key": {
				ExtKey:      "valid-key",
				TenantCode:  "TEST",
				ProductCode: "PROD",
				PackageCode: "PKG",
			},
		},
		tenants: map[string]*types.Tenant{
			"TEST": {Code: "TEST"},
		},
		products: map[string]*types.Product{
			"PROD": {Code: "PROD"},
		},
		packages: map[string]*types.Package{
			"PROD/PKG": {Code: "PKG"},
		},
	}
	reg := &mockRegistryMT{}

	called := false
	contextMW := Context(logger, "test")
	keyMW := Key(prov)
	mtMW := MT(prov, reg, nil, "test")

	handler := contextMW(keyMW(mtMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "valid-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called for valid tenant/product/package")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestMT_StoresTenantInContext(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{
			"ctx-key": {
				ExtKey:      "ctx-key",
				TenantCode:  "MYTEN",
				ProductCode: "PROD",
				PackageCode: "PKG",
			},
		},
		tenants: map[string]*types.Tenant{
			"MYTEN": {ID: "tenant-123", Code: "MYTEN", Name: "My Tenant"},
		},
		products: map[string]*types.Product{
			"PROD": {Code: "PROD"},
		},
		packages: map[string]*types.Package{
			"PROD/PKG": {Code: "PKG"},
		},
	}
	reg := &mockRegistryMT{}

	var capturedTenant *types.Tenant
	contextMW := Context(logger, "test")
	keyMW := Key(prov)
	mtMW := MT(prov, reg, nil, "test")

	handler := contextMW(keyMW(mtMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soajsCtx := GetSOAJSContext(r.Context())
		if soajsCtx != nil {
			capturedTenant = soajsCtx.Tenant
		}
		w.WriteHeader(http.StatusOK)
	}))))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "ctx-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if capturedTenant == nil {
		t.Fatal("Tenant should be stored in context")
	}
	if capturedTenant.Code != "MYTEN" {
		t.Errorf("Tenant.Code = %s, want MYTEN", capturedTenant.Code)
	}
}

// =============================================================================
// mergeACL Tests
// =============================================================================

func TestMergeACL_NilACLs(t *testing.T) {
	result := mergeACL(nil, nil, "oauth", "test")
	if result != nil {
		t.Error("Expected nil when both ACLs are nil")
	}
}

func TestMergeACL_PackageACLOnly(t *testing.T) {
	packageACL := map[string]interface{}{
		"oauth": map[string]interface{}{
			"access": true,
		},
	}

	result := mergeACL(packageACL, nil, "oauth", "test")
	if result == nil {
		t.Error("Expected non-nil result with package ACL")
	}
}

func TestMergeACL_KeyACLOverridesPackage(t *testing.T) {
	packageACL := map[string]interface{}{
		"oauth": map[string]interface{}{
			"access": true,
			"limit":  100,
		},
	}
	keyACL := map[string]interface{}{
		"oauth": map[string]interface{}{
			"limit": 200,
		},
	}

	result := mergeACL(packageACL, keyACL, "oauth", "test")
	if result == nil {
		t.Fatal("Expected non-nil result")
	}

	if limit, ok := result["limit"].(int); !ok || limit != 200 {
		t.Errorf("Key ACL should override package ACL, got limit=%v", result["limit"])
	}
}

func TestMergeACL_EnvironmentKeyedACL(t *testing.T) {
	packageACL := map[string]interface{}{
		"dashboard": map[string]interface{}{
			"oauth": map[string]interface{}{
				"access": true,
			},
		},
	}

	result := mergeACL(packageACL, nil, "oauth", "dashboard")
	if result == nil {
		t.Error("Expected to find environment-keyed ACL")
	}
}

func TestMergeACL_FallbackToDirectServiceKey(t *testing.T) {
	packageACL := map[string]interface{}{
		"oauth": map[string]interface{}{
			"access": false,
		},
	}

	result := mergeACL(packageACL, nil, "oauth", "nonexistent")
	if result == nil {
		t.Error("Expected to fallback to direct service key")
	}
}

// =============================================================================
// checkServiceAccess Tests
// =============================================================================

func TestCheckServiceAccess_NilACLAllowsAccess(t *testing.T) {
	if !checkServiceAccess(nil, "oauth", "1", "GET", "/token") {
		t.Error("Nil ACL should allow access")
	}
}

func TestCheckServiceAccess_ServiceExistsInACL(t *testing.T) {
	acl := map[string]interface{}{
		"1": map[string]interface{}{
			"access": true,
		},
	}

	if !checkServiceAccess(acl, "oauth", "1", "GET", "/token") {
		t.Error("Service in ACL should allow access")
	}
}

func TestCheckServiceAccess_APIPathInACL(t *testing.T) {
	acl := map[string]interface{}{
		"1": map[string]interface{}{
			"get": map[string]interface{}{
				"apis": map[string]interface{}{
					"/token": map[string]interface{}{
						"access": true,
					},
				},
			},
		},
	}

	if !checkServiceAccess(acl, "oauth", "1", "GET", "/token") {
		t.Error("API path in ACL should allow access")
	}
}

func TestCheckServiceAccess_VersionNotInACL(t *testing.T) {
	acl := map[string]interface{}{
		"1": map[string]interface{}{
			"access": true,
		},
	}

	// Version 2 not in ACL - should still allow (checked elsewhere)
	if !checkServiceAccess(acl, "oauth", "2", "GET", "/token") {
		t.Error("Missing version should default to allow")
	}
}

// =============================================================================
// requiresOAuth Tests
// =============================================================================

func TestRequiresOAuth_NilPackage(t *testing.T) {
	if requiresOAuth(nil, nil, "test") {
		t.Error("Nil package should not require OAuth")
	}
}

func TestRequiresOAuth_NilACL(t *testing.T) {
	pkg := &types.Package{ACL: nil}
	params := &types.ServiceParams{URLInfo: &types.URLInfo{}}

	if requiresOAuth(pkg, params, "test") {
		t.Error("Nil ACL should not require OAuth")
	}
}

func TestRequiresOAuth_AccessFalse(t *testing.T) {
	pkg := &types.Package{
		ACL: map[string]interface{}{
			"oauth": map[string]interface{}{
				"1": map[string]interface{}{
					"access": false,
				},
			},
		},
	}
	params := &types.ServiceParams{
		URLInfo: &types.URLInfo{
			ServiceName:    "oauth",
			ServiceVersion: "1",
			ServicePath:    "/token",
		},
	}

	if requiresOAuth(pkg, params, "test") {
		t.Error("access:false should not require OAuth")
	}
}

func TestRequiresOAuth_AccessTrue(t *testing.T) {
	pkg := &types.Package{
		ACL: map[string]interface{}{
			"oauth": map[string]interface{}{
				"1": map[string]interface{}{
					"access": true,
				},
			},
		},
	}
	params := &types.ServiceParams{
		URLInfo: &types.URLInfo{
			ServiceName:    "oauth",
			ServiceVersion: "1",
			ServicePath:    "/user",
		},
	}

	if !requiresOAuth(pkg, params, "test") {
		t.Error("access:true should require OAuth")
	}
}

func TestRequiresOAuth_AccessGroups(t *testing.T) {
	pkg := &types.Package{
		ACL: map[string]interface{}{
			"oauth": map[string]interface{}{
				"1": map[string]interface{}{
					"access": []interface{}{"admin", "user"},
				},
			},
		},
	}
	params := &types.ServiceParams{
		URLInfo: &types.URLInfo{
			ServiceName:    "oauth",
			ServiceVersion: "1",
			ServicePath:    "/admin",
		},
	}

	if !requiresOAuth(pkg, params, "test") {
		t.Error("access with groups should require OAuth")
	}
}

func TestRequiresOAuth_APILevelAccessOverridesVersion(t *testing.T) {
	pkg := &types.Package{
		ACL: map[string]interface{}{
			"oauth": map[string]interface{}{
				"1": map[string]interface{}{
					"access": true, // version level requires auth
					"get": map[string]interface{}{
						"apis": map[string]interface{}{
							"/public": map[string]interface{}{
								"access": false, // but this API is public
							},
						},
					},
				},
			},
		},
	}
	params := &types.ServiceParams{
		URLInfo: &types.URLInfo{
			ServiceName:    "oauth",
			ServiceVersion: "1",
			ServicePath:    "/public",
		},
	}

	if requiresOAuth(pkg, params, "test") {
		t.Error("API-level access:false should override version-level access:true")
	}
}

// =============================================================================
// isAccessTruthy Tests
// =============================================================================

func TestIsAccessTruthy_Nil(t *testing.T) {
	if isAccessTruthy(nil) {
		t.Error("nil should not be truthy")
	}
}

func TestIsAccessTruthy_False(t *testing.T) {
	if isAccessTruthy(false) {
		t.Error("false should not be truthy")
	}
}

func TestIsAccessTruthy_True(t *testing.T) {
	if !isAccessTruthy(true) {
		t.Error("true should be truthy")
	}
}

func TestIsAccessTruthy_EmptyArray(t *testing.T) {
	if isAccessTruthy([]interface{}{}) {
		t.Error("empty array should not be truthy")
	}
}

func TestIsAccessTruthy_NonEmptyArray(t *testing.T) {
	if !isAccessTruthy([]interface{}{"admin"}) {
		t.Error("non-empty array should be truthy")
	}
}

// =============================================================================
// UserACL Middleware Tests
// =============================================================================

func TestUserACL_SkipsWhenNoContext(t *testing.T) {
	userACLMW := UserACL()

	called := false
	handler := userACLMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func TestUserACL_SkipsWhenNoUserInfo(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	contextMW := Context(logger, "test")
	userACLMW := UserACL()

	called := false
	handler := contextMW(userACLMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("Handler should be called when no user info")
	}
}

// =============================================================================
// MT Whitelist Tests
// =============================================================================

func TestCheckMTWhitelist_NilRegistry(t *testing.T) {
	skipACL, skipOAuth := CheckMTWhitelist(nil, "192.168.1.1")
	if skipACL || skipOAuth {
		t.Error("Nil registry should not skip anything")
	}
}

func TestCheckMTWhitelist_NoWhitelistConfig(t *testing.T) {
	regData := &types.RegistryData{}
	skipACL, skipOAuth := CheckMTWhitelist(regData, "192.168.1.1")
	if skipACL || skipOAuth {
		t.Error("No whitelist config should not skip anything")
	}
}
