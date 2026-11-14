package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// mockRegistryMT implements registry.Registry for MT tests
type mockRegistryMT struct {
	data *types.RegistryData
}

func (m *mockRegistryMT) Load(env string) error                    { return nil }
func (m *mockRegistryMT) Reload() error                            { return nil }
func (m *mockRegistryMT) Get() *types.RegistryData                 { return m.data }
func (m *mockRegistryMT) RegisterHost(info types.HostRecord) error { return nil }
func (m *mockRegistryMT) GetHost(serviceName, version string) (*types.HostInfo, error) {
	return nil, nil
}
func (m *mockRegistryMT) GetEnvironment() string { return "test" }

// =============================================================================
// MT Middleware Tests
// =============================================================================

func TestMT_SkipsWhenNoKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	prov := &mockProvision{}
	reg := &mockRegistryMT{}

	contextMW := Context(logger, "test")
	mtMW := MT(prov, reg, "test")

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

func mtKey(extKey, pkg string) *types.Key {
	return &types.Key{
		InternalKey: "ikey",
		ExtKey:      extKey,
		Env:         "TEST",
		Tenant:      types.KeyTenant{ID: "tenant-123", Code: "MYTEN", Name: "My Tenant"},
		Application: types.KeyApplication{Product: "PROD", Package: pkg},
	}
}

func mtRegistry() *mockRegistryMT {
	return &mockRegistryMT{data: &types.RegistryData{
		ServiceConfig: &types.GlobalServiceConfig{},
		Services: map[string]*types.ServiceConfig{"svc": {Versions: map[string]*types.VersionInfo{
			"1": {ExtKeyRequired: true, OAuth: true},
		}}},
	}}
}

func runMT(t *testing.T, prov *mockProvision, extKey string) (*httptest.ResponseRecorder, *SOAJSContext) {
	t.Helper()
	reg := mtRegistry()
	var captured *SOAJSContext
	handler := Context(zap.NewNop(), "test")(URL(nil)(Key(prov, reg, "test")(KeyACL()(MT(prov, reg, "test")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = GetSOAJSContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))))))
	req := httptest.NewRequest("GET", "/svc/v1/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, extKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, captured
}

// A package that does not load answers 152, as in Node.js.
func TestMT_RejectsKeyWithoutPackage(t *testing.T) {
	prov := &mockProvision{keys: map[string]*types.Key{"pkg-key": mtKey("pkg-key", "PROD_MISSING")}}
	rec, _ := runMT(t, prov, "pkg-key")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"codes":[152]`) {
		t.Errorf("got %d %s, want 152", rec.Code, rec.Body.String())
	}
}

func TestMT_StoresTenantFromKey(t *testing.T) {
	prov := &mockProvision{
		keys: map[string]*types.Key{"ctx-key": mtKey("ctx-key", "PROD_PKG")},
		packages: map[string]*types.Package{"PROD_PKG": {Code: "PROD_PKG", ACL: map[string]interface{}{
			"svc": map[string]interface{}{"1": map[string]interface{}{"access": false}},
		}}},
	}
	rec, ctx := runMT(t, prov, "ctx-key")
	if rec.Code != http.StatusOK {
		t.Fatalf("Status = %d %s", rec.Code, rec.Body.String())
	}
	if ctx.Tenant == nil || ctx.Tenant.Code != "MYTEN" || ctx.Tenant.ID != "tenant-123" {
		t.Errorf("tenant = %+v, want the key's tenant", ctx.Tenant)
	}
	if ctx.Controller.ServiceParams.RequiresOAuth {
		t.Error("an open service version with no API listed is public")
	}
}

// apiCheck follows the Node.js mt apiCheck and checkPermission for boolean access.
func TestAPICheck(t *testing.T) {
	acl := func(access bool, perm string, apis map[string]bool) *types.FinalACL {
		f := &types.FinalACL{Access: access, APIsPermission: perm, APIs: map[string]*types.ACLAPI{}}
		for p, a := range apis {
			f.APIs[p] = &types.ACLAPI{Access: a, Route: p}
		}
		return f
	}
	tests := []struct {
		name     string
		acl      *types.FinalACL
		path     string
		loggedIn bool
		code     int
		public   bool
	}{
		{"open, unlisted", acl(false, "", nil), "/x", false, 0, true},
		{"open, restricted, unlisted", acl(false, "restricted", nil), "/x", false, 159, true},
		{"open, restricted, listed public", acl(false, "restricted", map[string]bool{"/x": false}), "/x", false, 0, true},
		{"open, restricted, listed private, no user", acl(false, "restricted", map[string]bool{"/x": true}), "/x", false, 161, false},
		{"open, restricted, listed private, user", acl(false, "restricted", map[string]bool{"/x": true}), "/x", true, 0, false},
		{"login, no user, unlisted", acl(true, "", nil), "/x", false, 158, false},
		{"login, no user, public api", acl(true, "", map[string]bool{"/x": false}), "/x", false, 0, true},
		{"login, user, restricted unlisted", acl(true, "restricted", nil), "/x", true, 159, false},
		{"login, user, listed", acl(true, "restricted", map[string]bool{"/x": true}), "/x", true, 0, false},
	}
	for _, tt := range tests {
		params := &types.ServiceParams{ACL: tt.acl, URLInfo: &types.URLInfo{ServicePath: tt.path}}
		if code := apiCheck(params, tt.loggedIn); code != tt.code || params.IsAPIPublic != tt.public {
			t.Errorf("%s: code %d public %v, want %d %v", tt.name, code, params.IsAPIPublic, tt.code, tt.public)
		}
	}
}

func TestIsAPIPublic(t *testing.T) {
	open := &types.FinalACL{Access: false}
	login := &types.FinalACL{Access: true, APIs: map[string]*types.ACLAPI{"/pub": {Access: false}}}
	req := httptest.NewRequest("GET", "/svc/x", nil)
	if !isAPIPublic(open, "/x", req) || isAPIPublic(login, "/x", req) || !isAPIPublic(login, "/pub", req) {
		t.Error("public decision")
	}
	if isAPIPublic(nil, "/x", req) {
		t.Error("a service the package does not grant is not public")
	}
	if isAPIPublic(open, "/x", httptest.NewRequest("GET", "/svc/x?access_token=t", nil)) {
		t.Error("a token in the query string makes the API private")
	}
}

func TestGeoAllowed(t *testing.T) {
	geo := map[string]interface{}{"allow": []interface{}{"10.0.0.0/8"}, "deny": []interface{}{"10.9.9.9"}}
	if !geoAllowed(nil, "1.2.3.4") || !geoAllowed(geo, "10.1.2.3") || geoAllowed(geo, "10.9.9.9") || geoAllowed(geo, "11.0.0.1") {
		t.Error("geo deny, then allow")
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

func (m *mockRegistryMT) OtherEnvControllerHosts(string) ([]types.HostRecord, error) {
	return nil, nil
}

func (m *mockRegistryMT) UpsertEnvController(string, string, int64, map[string]interface{}) error {
	return nil
}

func (m *mockRegistryMT) LoadByEnv(env string, reload bool) (*types.RegistryData, error) {
	return m.Get(), nil
}

func (m *mockRegistryMT) Close() {}

// A service the package does not grant answers 154 after OAuth, as in Node.js (serviceCheck).
func TestMTACL_ServiceNotInPackage(t *testing.T) {
	reg := mtRegistry()
	prov := &mockProvision{
		keys:     map[string]*types.Key{"k": mtKey("k", "PROD_PKG")},
		packages: map[string]*types.Package{"PROD_PKG": {Code: "PROD_PKG", ACL: map[string]interface{}{}}},
	}
	final := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	handler := Context(zap.NewNop(), "test")(URL(nil)(Key(prov, reg, "test")(KeyACL()(MT(prov, reg, "test")(MTACL(reg)(final))))))
	req := httptest.NewRequest("GET", "/svc/v1/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "k")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"codes":[154]`) {
		t.Errorf("got %d %s, want 154", rec.Code, rec.Body.String())
	}
}

// A key without a package answers 153.
func TestMT_KeyWithoutApplicationPackage(t *testing.T) {
	prov := &mockProvision{keys: map[string]*types.Key{"k": mtKey("k", "")}}
	rec, _ := runMT(t, prov, "k")
	if rec.Code != 500 || !strings.Contains(rec.Body.String(), `"codes":[153]`) {
		t.Errorf("got %d %s, want 153", rec.Code, rec.Body.String())
	}
}
