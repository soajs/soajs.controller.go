package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// mockProvision implements provision.Provision for testing
type mockProvision struct {
	keys     map[string]*types.Key
	packages map[string]*types.Package
	oauths   map[string]*types.TenantOauth
}

func (m *mockProvision) LoadProvision() error                    { return nil }
func (m *mockProvision) SetKeyConfig(algorithm, password string) {}
func (m *mockProvision) GetExternalKeyData(extKey string) (*types.Key, error) {
	if k, ok := m.keys[extKey]; ok {
		copy := *k
		return &copy, nil
	}
	return nil, context.DeadlineExceeded
}
func (m *mockProvision) GetPackageData(code string) (*types.Package, error) {
	if p, ok := m.packages[code]; ok {
		return p, nil
	}
	return nil, context.DeadlineExceeded
}
func (m *mockProvision) GetTenantOauth(id string) (*types.TenantOauth, error) {
	if o, ok := m.oauths[id]; ok {
		return o, nil
	}
	return nil, context.DeadlineExceeded
}
func (m *mockProvision) GetTenantCode(id string) (string, error) { return "", nil }
func (m *mockProvision) OAuthModel() provision.OAuthModel        { return nil }

func testKey(extKey, env, pkg string) *types.Key {
	return &types.Key{
		InternalKey: "ikey",
		ExtKey:      extKey,
		Env:         env,
		Tenant:      types.KeyTenant{ID: "tenant-id", Code: "TCODE"},
		Application: types.KeyApplication{Product: "PROD", Package: pkg, AppID: "app"},
		Config: map[string]interface{}{
			"gateway": map[string]interface{}{"networkPackages": map[string]interface{}{
				"net1": map[string]interface{}{"PROD": map[string]interface{}{"default": "PROD_NET"}},
			}},
		},
	}
}

func runKey(t *testing.T, prov provision.Provision, reg *mockRegistryMT, req *http.Request) (*httptest.ResponseRecorder, *SOAJSContext) {
	t.Helper()
	logger := zap.NewNop()
	var captured *SOAJSContext
	handler := Context(logger, "dev")(URL()(Key(prov, reg, "dev")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetSOAJSContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, captured
}

func TestKeyMiddleware_ValidKey(t *testing.T) {
	prov := &mockProvision{
		keys:     map[string]*types.Key{"ext": testKey("ext", "DEV", "PROD_DEFAULT")},
		packages: map[string]*types.Package{"PROD_DEFAULT": {Code: "PROD_DEFAULT"}},
	}
	req := httptest.NewRequest("GET", "/svc/v1/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "ext")
	rec, ctx := runKey(t, prov, &mockRegistryMT{}, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ctx.Key == nil || ctx.Key.ExtKey != "ext" {
		t.Error("the key is not in the context")
	}
	if ctx.Controller.ServiceParams.Package == nil || ctx.Controller.ServiceParams.Package.Code != "PROD_DEFAULT" {
		t.Error("the key's package is not loaded")
	}
	if ctx.ServicesConfig["gateway"] == nil {
		t.Error("the key's configuration must become the request's servicesConfig")
	}
}

// An external key that does not resolve answers 148, as in Node.js.
func TestKeyMiddleware_InvalidKey(t *testing.T) {
	req := httptest.NewRequest("GET", "/svc/v1/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "invalid-key")
	rec, _ := runKey(t, &mockProvision{}, &mockRegistryMT{}, req)
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"codes":[148]`) {
		t.Errorf("got %d %s, want 500 with 148", rec.Code, rec.Body.String())
	}
}

// An extKey of another environment answers 144.
func TestKeyMiddleware_OtherEnvironment(t *testing.T) {
	prov := &mockProvision{keys: map[string]*types.Key{"ext": testKey("ext", "STG", "PROD_DEFAULT")}}
	req := httptest.NewRequest("GET", "/svc/v1/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "ext")
	rec, _ := runKey(t, prov, &mockRegistryMT{}, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"codes":[144]`) {
		t.Errorf("got %d %s, want 401 with 144", rec.Code, rec.Body.String())
	}
}

// The custom registry's lastSeen.network picks the key's network package.
func TestKeyMiddleware_NetworkPackage(t *testing.T) {
	prov := &mockProvision{
		keys:     map[string]*types.Key{"ext": testKey("ext", "DEV", "PROD_DEFAULT")},
		packages: map[string]*types.Package{"PROD_NET": {Code: "PROD_NET"}},
	}
	reg := &mockRegistryMT{data: &types.RegistryData{CustomRegistry: map[string]interface{}{
		"gateway": map[string]interface{}{"value": map[string]interface{}{"lastSeen": map[string]interface{}{"network": "net1"}}},
	}}}
	req := httptest.NewRequest("GET", "/svc/v1/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "ext")
	rec, ctx := runKey(t, prov, reg, req)
	if rec.Code != http.StatusOK || ctx.Key.Application.Package != "PROD_NET" || ctx.Controller.ServiceParams.Package.Code != "PROD_NET" {
		t.Errorf("network package not applied: %d %+v", rec.Code, ctx.Key.Application)
	}
}

func TestKeyMiddleware_NoKey(t *testing.T) {
	req := httptest.NewRequest("GET", "/svc/v1/test", nil)
	rec, ctx := runKey(t, &mockProvision{}, &mockRegistryMT{}, req)
	if rec.Code != http.StatusOK || ctx.Key != nil {
		t.Errorf("no key: got %d", rec.Code)
	}
}

func TestKeyMiddleware_KeyFromQuery(t *testing.T) {
	prov := &mockProvision{
		keys:     map[string]*types.Key{"query-key": testKey("query-key", "DEV", "PROD_DEFAULT")},
		packages: map[string]*types.Package{"PROD_DEFAULT": {Code: "PROD_DEFAULT"}},
	}
	req := httptest.NewRequest("GET", "/svc/v1/test?key=query-key", nil)
	rec, ctx := runKey(t, prov, &mockRegistryMT{}, req)
	if rec.Code != http.StatusOK || ctx.Key == nil {
		t.Errorf("key from the query string: got %d", rec.Code)
	}
}
