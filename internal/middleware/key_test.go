package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// mockProvision implements provision.Provision for testing
type mockProvision struct {
	keys     map[string]*types.Key
	tenants  map[string]*types.Tenant
	products map[string]*types.Product
	packages map[string]*types.Package
}

func (m *mockProvision) LoadProvision() error                     { return nil }
func (m *mockProvision) SetKeyConfig(algorithm, password string)  {}
func (m *mockProvision) GetTenant(code string) (*types.Tenant, error) {
	if t, ok := m.tenants[code]; ok {
		return t, nil
	}
	return nil, context.DeadlineExceeded
}
func (m *mockProvision) GetKey(extKey string) (*types.Key, error) {
	if k, ok := m.keys[extKey]; ok {
		return k, nil
	}
	return nil, context.DeadlineExceeded
}
func (m *mockProvision) GetProduct(code string) (*types.Product, error) {
	if p, ok := m.products[code]; ok {
		return p, nil
	}
	return nil, context.DeadlineExceeded
}
func (m *mockProvision) GetPackage(productCode, packageCode string) (*types.Package, error) {
	key := productCode + "/" + packageCode
	if p, ok := m.packages[key]; ok {
		return p, nil
	}
	return nil, context.DeadlineExceeded
}
func (m *mockProvision) OAuthModel() provision.OAuthModel { return nil }

func TestKeyMiddleware_ValidKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{
			"test-key-123": {
				ExtKey:      "test-key-123",
				TenantCode:  "TCODE",
				TenantID:    "tenant-id",
				ProductCode: "PROD",
				PackageCode: "PKG",
			},
		},
	}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)

	var capturedKey *types.Key
	handler := contextMW(keyMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		soajsCtx := GetSOAJSContext(ctx)
		if soajsCtx != nil {
			capturedKey = soajsCtx.Key
		}
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "test-key-123")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	if capturedKey == nil {
		t.Error("Key was not attached to context")
	} else if capturedKey.ExtKey != "test-key-123" {
		t.Errorf("Key.ExtKey = %q, want %q", capturedKey.ExtKey, "test-key-123")
	}
}

func TestKeyMiddleware_InvalidKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{},
	}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)

	handler := contextMW(keyMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set(config.HeaderSOAJSKey, "invalid-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestKeyMiddleware_NoKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)

	handlerCalled := false
	handler := contextMW(keyMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	if !handlerCalled {
		t.Error("Handler was not called when no key provided")
	}
}

func TestKeyMiddleware_KeyFromQuery(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	prov := &mockProvision{
		keys: map[string]*types.Key{
			"query-key-456": {
				ExtKey:     "query-key-456",
				TenantCode: "TCODE",
			},
		},
	}

	contextMW := Context(logger, "test")
	keyMW := Key(prov)

	var capturedKey *types.Key
	handler := contextMW(keyMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		soajsCtx := GetSOAJSContext(ctx)
		if soajsCtx != nil {
			capturedKey = soajsCtx.Key
		}
		w.WriteHeader(http.StatusOK)
	})))

	req := httptest.NewRequest("GET", "/test?key=query-key-456", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}

	if capturedKey == nil {
		t.Error("Key from query param was not attached to context")
	}
}
