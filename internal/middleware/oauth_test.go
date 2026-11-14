package middleware

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

type mockOAuthModel struct {
	tokens map[string]*types.AccessToken
	err    error
}

func (m *mockOAuthModel) GetAccessToken(token string) (*types.AccessToken, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tokens[token], nil
}

type oauthProvision struct {
	mockProvision
	model *mockOAuthModel
}

func (p *oauthProvision) OAuthModel() provision.OAuthModel { return p.model }

// oauthCase is one request through OAuth, with mt's results set as mt would set them.
type oauthCase struct {
	method   string
	target   string
	headers  map[string]string
	noKey    bool
	keyAPI   bool
	tenOauth *types.TenantOauth
	custom   map[string]interface{}
	tokens   map[string]*types.AccessToken
	modelErr error
}

func runOAuth(t *testing.T, c oauthCase) (*httptest.ResponseRecorder, bool) {
	t.Helper()
	if c.method == "" {
		c.method = "GET"
	}
	if c.target == "" {
		c.target = "/svc/v1/test"
	}
	prov := &oauthProvision{model: &mockOAuthModel{tokens: c.tokens, err: c.modelErr}}
	reg := &mockRegistryMT{data: &types.RegistryData{CustomRegistry: c.custom}}
	setup := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := GetSOAJSContext(r.Context())
			params := ctx.Controller.ServiceParams
			params.RequiresOAuth = true
			params.KeyAPI = c.keyAPI
			params.TenantOauth = c.tenOauth
			if !c.noKey {
				key := testKey("ext-key", "TEST", "PROD_DEFAULT")
				ctx.Key = key
				ctx.Tenant = types.TenantFromKey(key)
			}
			next.ServeHTTP(w, r)
		})
	}
	called := false
	handler := Context(zap.NewNop(), "test")(URL(nil)(setup(OAuth(prov, reg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})))))
	req := httptest.NewRequest(c.method, c.target, nil)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, called
}

func validToken(user map[string]interface{}) map[string]*types.AccessToken {
	return map[string]*types.AccessToken{"tok": {Token: "tok", Expires: time.Now().Add(time.Hour), User: user}}
}

func expectAnswer(t *testing.T, name string, rec *httptest.ResponseRecorder, called bool, status int, text string) {
	t.Helper()
	if called {
		t.Errorf("%s: the request went through", name)
		return
	}
	if rec.Code != status || !strings.Contains(rec.Body.String(), text) {
		t.Errorf("%s: got %d %s, want %d with %q", name, rec.Code, rec.Body.String(), status, text)
	}
}

func expectPass(t *testing.T, name string, rec *httptest.ResponseRecorder, called bool) {
	t.Helper()
	if !called {
		t.Errorf("%s: refused with %d %s", name, rec.Code, rec.Body.String())
	}
}

func TestOAuth_NotRequired(t *testing.T) {
	prov := &oauthProvision{model: &mockOAuthModel{}}
	called := false
	handler := Context(zap.NewNop(), "test")(OAuth(prov, &mockRegistryMT{})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/svc/v1/test", nil))
	expectPass(t, "not required", rec, called)
}

// The oauth2-server 2.4.1 getBearerToken and checkToken answers, texts byte for byte.
func TestOAuth_BearerToken(t *testing.T) {
	tokens := validToken(map[string]interface{}{"_id": "u1"})
	tokens["old"] = &types.AccessToken{Token: "old", Expires: time.Now().Add(-time.Second)}

	rec, called := runOAuth(t, oauthCase{tokens: tokens})
	expectAnswer(t, "no token", rec, called, 400, `"code":400,"message":"The access token was not found"`)

	rec, called = runOAuth(t, oauthCase{tokens: tokens, target: "/svc/v1/test?access_token=tok", headers: map[string]string{"Authorization": "Bearer tok"}})
	expectAnswer(t, "two methods", rec, called, 400, "Only one method may be used to authenticate at a time (Auth header,  GET or POST).")

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"access_token": "tok", "Authorization": "Bearer tok"}})
	expectAnswer(t, "two methods, access_token header", rec, called, 400, "Only one method may be used")

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"Authorization": "Basic dXNlcjpwYXNz"}})
	expectAnswer(t, "basic auth", rec, called, 400, "Malformed auth header")

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"Authorization": ""}})
	expectAnswer(t, "empty auth header", rec, called, 401, "The access token provided is invalid.")

	rec, called = runOAuth(t, oauthCase{tokens: tokens, target: "/svc/v1/test?access_token="})
	expectAnswer(t, "empty access_token", rec, called, 401, "The access token provided is invalid.")

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"Authorization": "Bearer nope"}})
	expectAnswer(t, "unknown token", rec, called, 401, `"code":401,"message":"The access token provided is invalid."`)

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"Authorization": "Bearer old"}})
	expectAnswer(t, "expired token", rec, called, 401, "The access token provided has expired.")

	rec, called = runOAuth(t, oauthCase{modelErr: errors.New("db down"), headers: map[string]string{"Authorization": "Bearer tok"}})
	expectAnswer(t, "model error", rec, called, 503, `"code":503`)

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"Authorization": "Bearer tok"}})
	expectPass(t, "bearer", rec, called)
	rec, called = runOAuth(t, oauthCase{tokens: tokens, target: "/svc/v1/test?access_token=tok"})
	expectPass(t, "query", rec, called)
	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"access_token": "tok"}})
	expectPass(t, "access_token header", rec, called)
}

func TestOAuth_DeviceID(t *testing.T) {
	tokens := validToken(map[string]interface{}{"deviceId": "dev-1"})
	bearer := map[string]string{"Authorization": "Bearer tok"}

	rec, called := runOAuth(t, oauthCase{tokens: tokens, headers: bearer})
	expectAnswer(t, "no device-id", rec, called, 403, `"code":156`)

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"Authorization": "Bearer tok", "device-id": "dev-2"}})
	expectAnswer(t, "other device-id", rec, called, 403, `"code":156`)

	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: map[string]string{"Authorization": "Bearer tok", "device-id": "dev-1"}})
	expectPass(t, "same device-id", rec, called)

	off := map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"oauth": map[string]interface{}{"deviceIdCheck": false}}}}
	rec, called = runOAuth(t, oauthCase{tokens: tokens, headers: bearer, custom: off})
	expectPass(t, "deviceIdCheck false", rec, called)

	rec, called = runOAuth(t, oauthCase{tokens: validToken(map[string]interface{}{}), headers: bearer})
	expectPass(t, "token without deviceId", rec, called)
}

func TestOAuth_RestrictedTo(t *testing.T) {
	cases := []struct {
		name         string
		restrictedTo map[string]interface{}
		noKey        bool
		agent        string
		pass         bool
	}{
		{"tenant", map[string]interface{}{"tenant": "tenant-id"}, false, "", true},
		{"other tenant", map[string]interface{}{"tenant": "other"}, false, "", false},
		{"product list", map[string]interface{}{"product": []interface{}{"X", "PROD"}}, false, "", true},
		{"package", map[string]interface{}{"package": "PROD_OTHER"}, false, "", false},
		{"key", map[string]interface{}{"key": "ext-key"}, false, "", true},
		{"env, any case", map[string]interface{}{"env": "TEST"}, false, "", true},
		{"env list, any case", map[string]interface{}{"env": []interface{}{"dev", "Test"}}, false, "", true},
		{"other env", map[string]interface{}{"env": "dev"}, false, "", false},
		{"agent", map[string]interface{}{"agent": "MyApp/1"}, false, "MyApp/1", true},
		{"agent, case matters", map[string]interface{}{"agent": "myapp/1"}, false, "MyApp/1", false},
		{"agent, none sent", map[string]interface{}{"agent": "MyApp/1"}, false, "", false},
		{"falsy restriction is not checked", map[string]interface{}{"tenant": ""}, false, "", true},
		{"no key", map[string]interface{}{"tenant": "tenant-id"}, true, "", false},
		{"empty restrictedTo without a key", map[string]interface{}{}, true, "", false},
	}
	for _, c := range cases {
		headers := map[string]string{"Authorization": "Bearer tok"}
		if c.agent != "" {
			headers["User-Agent"] = c.agent
		}
		rec, called := runOAuth(t, oauthCase{tokens: validToken(map[string]interface{}{"restrictedTo": c.restrictedTo}), headers: headers, noKey: c.noKey})
		if c.pass {
			expectPass(t, c.name, rec, called)
		} else {
			expectAnswer(t, c.name, rec, called, 500, `"code":147`) // 147 has no status: 500, as Node.js answers it
		}
	}
	rec, called := runOAuth(t, oauthCase{tokens: validToken(map[string]interface{}{}), headers: map[string]string{"Authorization": "Bearer tok"}, noKey: true})
	expectPass(t, "unrestricted token without a key", rec, called)
}

func TestOAuth_Pin(t *testing.T) {
	pinOn := &types.TenantOauth{Pin: map[string]types.TenantOauthPin{"PROD": {Enabled: true}}}
	pinOff := &types.TenantOauth{Pin: map[string]types.TenantOauthPin{"PROD": {Enabled: false}}}
	bearer := map[string]string{"Authorization": "Bearer tok"}
	urac := func(extra map[string]interface{}) map[string]*types.AccessToken {
		user := map[string]interface{}{"loginMode": "urac"}
		for k, v := range extra {
			user[k] = v
		}
		return validToken(user)
	}
	custom := map[string]interface{}{"oauth": map[string]interface{}{"value": map[string]interface{}{
		"pinWrapper": map[string]interface{}{"servicename": "svc", "apiname": "/pinlogin"},
		"pinWhitelist": map[string]interface{}{"svc": map[string]interface{}{
			"get":  map[string]interface{}{"apis": []interface{}{"/open"}, "regex": []interface{}{"/item/:id"}},
			"post": map[string]interface{}{"apis": []interface{}{"/posted"}},
		}},
	}}}

	cases := []struct {
		name   string
		method string
		target string
		tokens map[string]*types.AccessToken
		oauth  *types.TenantOauth
		keyAPI bool
		pass   bool
	}{
		{"pin required, no pin login", "GET", "/svc/v1/test", urac(nil), pinOn, true, false},
		{"pin required, pin login", "GET", "/svc/v1/test", urac(map[string]interface{}{"pinLogin": true}), pinOn, true, true},
		{"pinLogin truthy", "GET", "/svc/v1/test", urac(map[string]interface{}{"pinLogin": 1.0}), pinOn, true, true},
		{"pin off for the product", "GET", "/svc/v1/test", urac(nil), pinOff, true, true},
		{"no tenant pin", "GET", "/svc/v1/test", urac(nil), &types.TenantOauth{}, true, true},
		{"not a urac login", "GET", "/svc/v1/test", validToken(map[string]interface{}{"loginMode": "oauth"}), pinOn, true, true},
		{"not a key API", "GET", "/svc/v1/test", urac(nil), pinOn, false, true},
		{"pin locked", "GET", "/svc/v1/test", urac(map[string]interface{}{"pinLocked": true, "pinLogin": true}), pinOff, true, false},
		{"pin locked, truthy", "GET", "/svc/v1/test", urac(map[string]interface{}{"pinLocked": "yes"}), pinOff, true, false},
		{"oauth pin API", "POST", "/oauth/pin", urac(nil), pinOn, true, true},
		{"oauth other API", "POST", "/oauth/token", urac(nil), pinOn, true, false},
		{"gateway", "GET", "/controller/x", urac(nil), pinOn, true, true},
		{"pinWrapper", "POST", "/svc/pinlogin", urac(nil), pinOn, true, true},
		{"whitelist api", "GET", "/svc/open", urac(nil), pinOn, true, true},
		{"whitelist api, other method", "DELETE", "/svc/open", urac(nil), pinOn, true, false},
		{"whitelist regex", "GET", "/svc/item/42", urac(nil), pinOn, true, true},
		{"whitelist regex, no match", "GET", "/svc/items/42", urac(nil), pinOn, true, false},
		{"whitelist post", "POST", "/svc/posted", urac(nil), pinOn, true, true},
	}
	for _, c := range cases {
		rec, called := runOAuth(t, oauthCase{method: c.method, target: c.target, tokens: c.tokens, tenOauth: c.oauth, keyAPI: c.keyAPI, headers: bearer, custom: custom})
		if c.pass {
			expectPass(t, c.name, rec, called)
		} else {
			expectAnswer(t, c.name, rec, called, 401, `"code":145`)
		}
	}
}
