package middleware

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

func TestFinalACLOf(t *testing.T) {
	acl := map[string]interface{}{
		// the latest version; restricted, with {apis} and grouped entries
		"a": map[string]interface{}{
			"1": map[string]interface{}{"access": true},
			"2": map[string]interface{}{
				"access": false, "apisPermission": "restricted",
				"get":  map[string]interface{}{"apis": map[string]interface{}{"/z": map[string]interface{}{}, "/b": map[string]interface{}{}}},
				"post": []interface{}{map[string]interface{}{"group": "G", "apis": map[string]interface{}{"/p": map[string]interface{}{}}}, map[string]interface{}{"apis": map[string]interface{}{"/p": map[string]interface{}{}, "/q": map[string]interface{}{}}}},
				"put":  map[string]interface{}{"apis": map[string]interface{}{}},
			},
		},
		// not restricted: the version only
		"b": map[string]interface{}{"1": map[string]interface{}{"access": true, "get": map[string]interface{}{"apis": map[string]interface{}{"/x": map[string]interface{}{}}}}},
		// a version that is not an object is skipped; a service without one is left out
		"c": map[string]interface{}{"access": true},
	}
	got, restricted := finalACLOf(acl, zap.NewNop())
	want := map[string]interface{}{
		"a": map[string]interface{}{"version": "2", "get": []string{"/b", "/z"}, "post": []string{"/p", "/q"}},
		"b": map[string]interface{}{"version": "1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("finalACL\n got  %v\n want %v", got, want)
	}
	if !restricted["a"] || restricted["b"] {
		t.Errorf("restricted %v", restricted)
	}
	if f, _ := finalACLOf(nil, zap.NewNop()); f != nil {
		t.Error("no ACL, no finalACL")
	}
}

func TestQueryString(t *testing.T) {
	q := parseQueryString("b=2&proxyRoute=%2Fx%253F&a=x+y&b=3&bad=%zz&&flag")
	if q.get("b") != "3" || q.get("a") != "x y" || q.get("bad") != "%zz" || q.get("proxyRoute") != "/x%3F" {
		t.Errorf("parsed %v", q.values)
	}
	q.remove("proxyRoute")
	q.set("access_token", "t")
	if got := q.encode(); got != "b=3&a=x%20y&bad=%25zz&flag=&access_token=t" {
		t.Errorf("encoded %s", got)
	}
	if got := escapeComponent("a-_.!~*'()/ é"); got != "a-_.!~*'()%2F%20%C3%A9" {
		t.Errorf("escaped %s", got)
	}
	if _, err := decodeURIComponent("/a%2"); err == nil {
		t.Error("a % that does not start an escape")
	}
	if v, err := decodeURIComponent("/a%2Fb"); err != nil || v != "/a/b" {
		t.Errorf("decoded %q %v", v, err)
	}
}

// soajsProxyEnv is a gateway with GotoService and the proxy, the key's tenant set as mt does.
func soajsProxyEnv(t *testing.T, prov *mockProvision, tenant *types.Tenant) (*proxyEnv, func(*http.Request) *httptest.ResponseRecorder) {
	e := newProxyEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Service", "yes")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("proxied"))
	})
	serve := func(req *http.Request) *httptest.ResponseRecorder {
		handler := Context(zap.NewNop(), "test")(URL(nil)(GotoService(e.reg, e.aw, e.monitor(), prov, GatewayService{Name: "controller", Version: "1"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := GetSOAJSContext(r.Context())
			ctx.Tenant = tenant
			ctx.Controller.Gotoservice(w, r)
		}))))
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	return e, serve
}

func TestSoajsProxy_FullURI(t *testing.T) {
	e, serve := soajsProxyEnv(t, &mockProvision{}, nil)
	route := "http://127.0.0.1:" + strconv.Itoa(e.port) + "/a/../b%20c?dropped=1"
	req := httptest.NewRequest("POST", "/soajs/proxy?z=1&proxyRoute="+escapeComponent(route)+"&__env=&x=y", strings.NewReader("body"))
	req.Header.Set("access_token", "tok")
	req.Header.Set("key", testExtKey)
	req.Header.Set("Host", "gateway.example")
	rec := serve(req)
	if rec.Code != http.StatusAccepted || rec.Body.String() != "proxied" || rec.Header().Get("X-Service") != "yes" {
		t.Fatalf("answer %d %s", rec.Code, rec.Body.String())
	}
	got := e.got()
	if got.URL.Path != "/b c" || got.URL.RawQuery != "z=1&x=y&access_token=tok" {
		t.Errorf("called %s ? %s", got.URL.Path, got.URL.RawQuery)
	}
	if got.Header.Get("soajs_roaming") != "test" || got.Header.Get("key") != "" || got.Host == "gateway.example" {
		t.Errorf("headers %v host %s", got.Header, got.Host)
	}
	if body, _ := e.body.Load().(string); body != "body" {
		t.Errorf("body %q", body)
	}
}

func TestSoajsProxy_Codes(t *testing.T) {
	prov := &mockProvision{tenantExtKeys: map[string]map[string]string{"TEN": {"REMOTE": "remote-key"}}}
	tenant := &types.Tenant{Code: "TEN"}
	e, serve := soajsProxyEnv(t, prov, tenant)
	e.reg.others = map[string]*types.RegistryData{
		"remote": {Protocol: "http", Domain: "127.0.0.1", Port: e.port},
		"bare":   {},
	}
	code := func(path string) string {
		rec := serve(httptest.NewRequest("GET", path, nil))
		var body struct {
			Errors struct{ Codes []int } `json:"errors"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		if len(body.Errors.Codes) == 0 {
			return strconv.Itoa(rec.Code)
		}
		return strconv.Itoa(body.Errors.Codes[0])
	}
	cases := []struct{ path, want string }{
		{"/soajs/proxy", "139"},
		{"/soajs/proxy?proxyRoute=%25zz", "139"},
		{"/soajs/proxy?proxyRoute=%2Fsvc%2Fx", "135"},
		{"/soajs/proxy?proxyRoute=ftp%3A%2F%2Fhost%2Fx", "135"},
		{"/soajs/proxy?proxyRoute=%2Fx&__env=remote&tCode=OTHER", "139"},
		{"/soajs/proxy?proxyRoute=%2Fx&__env=bare", "137"},
		{"/soajs/proxy?proxyRoute=%2Fx&__env=bare&extKey=k", "208"},
		{"/soajs/proxy?proxyRoute=%2Fx&__env=nowhere&extKey=k", "207"},
		{"/soajs/proxy?proxyRoute=%2Fx&__env=remote", "202"},
	}
	for _, c := range cases {
		if got := code(c.path); got != c.want {
			t.Errorf("%s: %s, want %s", c.path, got, c.want)
		}
	}
	// the tenant's extKey for the environment is sent
	if got := e.got(); got.Header.Get("key") != "remote-key" || got.URL.Path != "/x" {
		t.Errorf("remote call %s key %q", got.URL.Path, got.Header.Get("key"))
	}
	// the environment from the header; no gateway entry
	delete(e.reg.data.Services, "controller")
	req := httptest.NewRequest("GET", "/soajs/proxy?proxyRoute=%2Fx", nil)
	req.Header.Set("__env", "remote")
	if rec := serve(req); !strings.Contains(rec.Body.String(), `"codes":[131]`) {
		t.Errorf("no gateway entry: %s", rec.Body.String())
	}
}

// /soajs/acl through the mt chain: a refusal before the user is loaded is 158.
func TestSoajsACL_GatewayAPI(t *testing.T) {
	key := mtKey("acl-key", "PROD_PKG")
	key.Geo = map[string]interface{}{"deny": []interface{}{"192.0.2.1"}}
	prov := &mockProvision{
		keys:     map[string]*types.Key{"acl-key": key},
		packages: map[string]*types.Package{"PROD_PKG": {Code: "PROD_PKG", ACL: map[string]interface{}{"svc": map[string]interface{}{"1": map[string]interface{}{"access": true}}}}},
	}
	reg := mtRegistry()
	e := newProxyEnv(t, nil)
	var reached *SOAJSContext
	handler := Context(zap.NewNop(), "test")(URL(nil)(Key(prov, reg, "test")(KeyACL()(GotoService(reg, e.aw, nil, prov, GatewayService{Name: "controller", Version: "1"})(MT(prov, reg, "test")(MTACL(reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = GetSOAJSContext(r.Context())
	}))))))))
	run := func(remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/soajs/acl", nil)
		req.Header.Set("key", "acl-key")
		req.RemoteAddr = remote + ":1234"
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := run("192.0.2.1"); !strings.Contains(rec.Body.String(), `"codes":[158]`) {
		t.Errorf("geo refusal: %s", rec.Body.String())
	}
	// no user: OAuth is required, and without it there is no user (158)
	reached = nil
	rec := run("192.0.2.2")
	if !strings.Contains(rec.Body.String(), `"codes":[158]`) || reached != nil {
		t.Errorf("no user: %s", rec.Body.String())
	}
}

func TestSoajsACL_Answer(t *testing.T) {
	soajsCtx := &SOAJSContext{
		Log: zap.NewNop(),
		Key: mtKey("k", "PKG"),
		Controller: &ControllerContext{ServiceParams: &types.ServiceParams{
			Package: &types.Package{ACL: map[string]interface{}{"svc": map[string]interface{}{"1": map[string]interface{}{"access": true}}}},
		}},
	}
	rec := httptest.NewRecorder()
	soajsACL(rec, soajsCtx)
	body, _ := io.ReadAll(rec.Body)
	want := `{"acl":{"svc":{"1":{"access":true}}},"finalACL":{"svc":{"version":"1"}},"packages":["PKG"],"tenant":{"code":"MYTEN","id":"tenant-123","name":"My Tenant","type":""},"urac":null}`
	if rec.Code != 200 || string(body) != want {
		t.Errorf("answer %d %s", rec.Code, body)
	}
}
