package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

func keyACLPackage() *types.Package {
	return &types.Package{Code: "P_D", ACL: map[string]interface{}{
		"orders": map[string]interface{}{
			"1": map[string]interface{}{"access": false},
			"10": map[string]interface{}{
				"access":         true,
				"apisPermission": "restricted",
				"get": map[string]interface{}{"apis": map[string]interface{}{
					"/list":      map[string]interface{}{"access": false, "group": "Orders"},
					"/order/:id": map[string]interface{}{"access": true},
				}},
			},
			"9": map[string]interface{}{"access": false},
		},
	}}
}

func runKeyACL(t *testing.T, method, uri string) (*httptest.ResponseRecorder, *types.ServiceParams) {
	t.Helper()
	var params *types.ServiceParams
	inner := KeyACL()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params = GetSOAJSContext(r.Context()).Controller.ServiceParams
		w.WriteHeader(200)
	}))
	handler := Context(zap.NewNop(), "dev")(URL(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := GetSOAJSContext(r.Context())
		ctx.Key = &types.Key{}
		ctx.Controller.ServiceParams.Package = keyACLPackage()
		inner.ServeHTTP(w, r)
	})))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(method, uri, nil))
	return rec, params
}

func TestKeyACLLatestVersion(t *testing.T) {
	rec, params := runKeyACL(t, "GET", "/orders/list")
	if rec.Code != 200 || params.URLInfo.ServiceVersion != "10" {
		t.Fatalf("the latest version the package grants (10 > 9): %d %q", rec.Code, params.URLInfo.ServiceVersion)
	}
	acl := params.ACL
	if !acl.Access || acl.APIsPermission != "restricted" {
		t.Errorf("version access and apisPermission: %+v", acl)
	}
	if api := acl.API("/list"); api == nil || api.Access || api.Group != "Orders" {
		t.Errorf("exact route: %+v", api)
	}
	if api := acl.API("/order/42"); api == nil || !api.Access {
		t.Error("a route with a path parameter matches as an expression")
	}
	if acl.API("/order/42/x") != nil || acl.API("/other") != nil {
		t.Error("unlisted paths have no API ACL")
	}
}

func TestKeyACLRequestedVersion(t *testing.T) {
	rec, params := runKeyACL(t, "POST", "/orders/v9/list")
	if rec.Code != 200 || params.ACL == nil || params.ACL.Access || params.ACL.APIs != nil {
		t.Errorf("version 9, no APIs for post: %d %+v", rec.Code, params.ACL)
	}
	rec, _ = runKeyACL(t, "GET", "/orders/v2/list")
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"codes":[154]`) {
		t.Errorf("a version the package does not have answers 154: %d %s", rec.Code, rec.Body.String())
	}
}

func TestKeyACLServiceNotInPackage(t *testing.T) {
	rec, params := runKeyACL(t, "GET", "/payments/v1/x")
	if rec.Code != 200 || params.ACL != nil {
		t.Error("a service the package does not grant has no final ACL; mt refuses it")
	}
}
