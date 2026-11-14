package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/urac"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

type uracStore struct {
	users map[string]map[string]interface{}
}

func (s *uracStore) FindUser(_ context.Context, filter bson.M) (map[string]interface{}, error) {
	u, ok := s.users[filter["username"].(string)]
	if !ok {
		return nil, nil
	}
	raw, _ := json.Marshal(u)
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func (s *uracStore) FindGroups(context.Context, bson.M) ([]map[string]interface{}, error) {
	return []map[string]interface{}{{"code": "g", "config": map[string]interface{}{"allowedPackages": map[string]interface{}{"PROD": []interface{}{"PROD_USER"}}}}}, nil
}

func jsonDoc(s string) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

type uracCase struct {
	username  string
	email     string
	env       string
	clientID  string
	keyConfig map[string]interface{}
	tenantErr bool
	version   string
}

// runUrac runs Key, KeyACL, MT, the token as OAuth would set it, then Urac.
func runUrac(t *testing.T, c uracCase) (*httptest.ResponseRecorder, *SOAJSContext) {
	t.Helper()
	key := testKey("ext", "DEV", "PROD_DEFAULT")
	key.Config = c.keyConfig
	prov := &mockProvision{
		keys: map[string]*types.Key{"ext": key},
		packages: map[string]*types.Package{
			"PROD_DEFAULT": {Code: "PROD_DEFAULT", ACL: jsonDoc(`{"svc":{"1":{"access":true},"2":{"access":true},"3":{"access":true}}}`)},
			"PROD_USER":    {Code: "PROD_USER", ACL: jsonDoc(`{"svc":{"1":{"access":true,"apisPermission":"restricted"},"2":{"access":true,"get":{"apis":{"/test":{"access":true}}}}}}`)},
		},
		oauths: map[string]*types.TenantOauth{"tenant-id": {}},
	}
	reg := &mockRegistryMT{data: &types.RegistryData{
		ServiceConfig: &types.GlobalServiceConfig{},
		Services: map[string]*types.ServiceConfig{"svc": {Versions: map[string]*types.VersionInfo{
			"1": {ExtKeyRequired: true, OAuth: true}, "2": {ExtKeyRequired: true, OAuth: true}, "3": {ExtKeyRequired: true, OAuth: true},
		}}},
	}}
	users := &uracStore{users: map[string]map[string]interface{}{
		"active": jsonDoc(`{"_id":"u1","username":"active","email":"now@x.io","status":"active","tenant":{"id":"tenant-id","code":"TCODE"},"groups":["g"],"config":{"keys":{"ikey":{"config":{"svc":{"user":true},"commonFields":{"shared":"user"}}}}},"profile":{}}`),
		"paused": jsonDoc(`{"_id":"u2","username":"paused","status":"inactive","tenant":{"id":"tenant-id","code":"TCODE"},"groups":[],"config":{},"profile":{}}`),
	}}
	driver := urac.NewDriverWithStore(users, zap.NewNop())
	env := c.env
	if env == "" {
		env = "dev"
	}
	clientID := c.clientID
	if clientID == "" {
		clientID = "tenant-id"
	}
	token := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			params := GetSOAJSContext(r.Context()).Controller.ServiceParams
			params.OAuthToken = &types.AccessToken{ClientID: clientID, Env: env, Expires: time.Now().Add(time.Hour), User: map[string]interface{}{
				"id": "6a0e000000000000000000ab", "username": c.username, "loginMode": "urac", "email": c.email,
				"tenant": map[string]interface{}{"id": "tenant-id", "code": "TCODE"},
			}}
			next.ServeHTTP(w, r)
		})
	}
	var captured *SOAJSContext
	var provision = prov
	if c.tenantErr {
		provision = &mockProvision{keys: prov.keys, packages: prov.packages, oauths: prov.oauths}
	}
	handler := Context(zap.NewNop(), "dev")(URL(nil)(Key(prov, reg, "dev")(KeyACL()(MT(prov, reg, "dev")(token(Urac(driver, provision, reg, "dev")(MTACL(reg)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = GetSOAJSContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})))))))))
	path := "/svc/test"
	if c.version != "" {
		path = "/svc/v" + c.version + "/test"
	}
	req := httptest.NewRequest("GET", path, nil)
	req.Header.Set("key", "ext")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec, captured
}

func TestUrac_ActiveUser(t *testing.T) {
	keyConfig := jsonDoc(`{"svc":{"key":true},"commonFields":{"shared":"key","keyOnly":1}}`)
	before, _ := json.Marshal(keyConfig)
	rec, ctx := runUrac(t, uracCase{username: "active", keyConfig: keyConfig})
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	want := jsonDoc(`{"svc":{"key":true,"user":true},"commonFields":{"shared":"user","keyOnly":1}}`)
	if !reflect.DeepEqual(ctx.ServicesConfig, want) {
		t.Errorf("services config %v", ctx.ServicesConfig)
	}
	if after, _ := json.Marshal(keyConfig); string(after) != string(before) {
		t.Errorf("the key config was modified: %s", after)
	}
	// the user's ACL replaces the package's: the latest version it grants becomes the request's
	if ctx.Controller.ServiceParams.URLInfo.ServiceVersion != "2" {
		t.Errorf("version %q", ctx.Controller.ServiceParams.URLInfo.ServiceVersion)
	}
}

func TestUrac_UserACLRefuses(t *testing.T) {
	// version 1 of the user's ACL is restricted and lists no API; the package's grants it
	rec, _ := runUrac(t, uracCase{username: "active", version: "1"})
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"code":159`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	// a version the package has and the user's ACL does not: no final ACL
	rec, _ = runUrac(t, uracCase{username: "active", version: "3"})
	if !strings.Contains(rec.Body.String(), `"code":154`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestUrac_Refused(t *testing.T) {
	for _, c := range []uracCase{
		{username: "paused"},
		{username: "ghost"},
		{username: "active", email: "before@x.io", keyConfig: jsonDoc(`{"gateway":{"validateTokenAccount":{"email":true}}}`)},
	} {
		rec, _ := runUrac(t, c)
		if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"code":146`) {
			t.Errorf("%+v: %d %s", c, rec.Code, rec.Body.String())
		}
	}
	rec, _ := runUrac(t, uracCase{username: "active", email: "now@x.io", keyConfig: jsonDoc(`{"gateway":{"validateTokenAccount":{"email":true}}}`)})
	if rec.Code != 200 {
		t.Errorf("same email: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUrac_Roaming(t *testing.T) {
	// a dashboard token in dev: the token's user is the record, the user is not read
	rec, ctx := runUrac(t, uracCase{username: "ghost", env: "dashboard"})
	if rec.Code != 200 || ctx.Roaming == nil || ctx.Roaming.Code != "TCODE" {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
	// its client tenant does not load: 170
	rec, _ = runUrac(t, uracCase{username: "ghost", env: "dashboard", clientID: "gone", tenantErr: true})
	if !strings.Contains(rec.Body.String(), `"code":170`) {
		t.Errorf("%d %s", rec.Code, rec.Body.String())
	}
}

func TestMergeRecursive(t *testing.T) {
	base := jsonDoc(`{"a":{"x":1,"l":[1,2]},"b":1}`)
	got := mergeRecursive(deepCopyJSON(base), jsonDoc(`{"a":{"y":2,"l":[3]},"b":{"o":1}}`))
	if !reflect.DeepEqual(got, jsonDoc(`{"a":{"x":1,"y":2,"l":[3]},"b":{"o":1}}`)) {
		t.Errorf("got %v", got)
	}
	if !reflect.DeepEqual(base, jsonDoc(`{"a":{"x":1,"l":[1,2]},"b":1}`)) {
		t.Errorf("base modified: %v", base)
	}
}
