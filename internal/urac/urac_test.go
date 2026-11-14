package urac

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// fakeStore answers from documents in memory, as the urac database would.
type fakeStore struct {
	users   []map[string]interface{}
	groups  []map[string]interface{}
	filters []bson.M
	err     error
}

func (s *fakeStore) FindUser(_ context.Context, filter bson.M) (map[string]interface{}, error) {
	s.filters = append(s.filters, filter)
	if s.err != nil {
		return nil, s.err
	}
	for _, u := range s.users {
		match := true
		for k, v := range filter {
			if k == "_id" {
				if u["_id"] != v.(interface{ Hex() string }).Hex() {
					match = false
				}
			} else if u[k] != v {
				match = false
			}
		}
		if match {
			return copyJSON(u), nil
		}
	}
	return nil, nil
}

func (s *fakeStore) FindGroups(_ context.Context, filter bson.M) ([]map[string]interface{}, error) {
	s.filters = append(s.filters, filter)
	codes := filter["code"].(bson.M)["$in"].([]interface{})
	var out []map[string]interface{}
	for _, g := range s.groups {
		tenant := g["tenant"].(map[string]interface{})
		if tenant["id"] != filter["tenant.id"] {
			continue
		}
		for _, c := range codes {
			if g["code"] == c {
				out = append(out, copyJSON(g))
			}
		}
	}
	return out, nil
}

func copyJSON(m map[string]interface{}) map[string]interface{} {
	raw, _ := json.Marshal(m)
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func doc(s string) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

const (
	home    = "6a0e00000000000000000002"
	other   = "6a0e00000000000000000001"
	ownerID = "6a0e000000000000000000ac"
)

func testStore() *fakeStore {
	return &fakeStore{
		users: []map[string]interface{}{
			doc(`{"_id":"` + ownerID + `","username":"owner","email":"owner@x.io","status":"active","password":"secret","tenant":{"id":"` + home + `","code":"DBTN"},"groups":["owner","ops"],"config":{"keys":{"ikey":{"config":{"svc":{"a":1}}}}},"profile":{}}`),
			doc(`{"_id":"6a0e000000000000000000b1","username":"paused","status":"inactive","tenant":{"id":"` + home + `","code":"DBTN"},"groups":[],"config":{},"profile":{}}`),
			doc(`{"_id":"6a0e000000000000000000b2","username":"pending","status":"pendingJoin","tenant":{"id":"` + home + `","code":"DBTN"},"groups":[],"config":{},"profile":{}}`),
			doc(`{"_id":"6a0e000000000000000000b3","username":"invited","status":"active","tenant":{"id":"` + other + `","code":"CONF"},"groups":["homegroup"],"config":{"allowedTenants":[{"tenant":{"id":"` + home + `","code":"DBTN"},"groups":["admin"]}]},"profile":{}}`),
			doc(`{"_id":"6a0e000000000000000000b4","username":"invitednogroups","status":"active","tenant":{"id":"` + other + `","code":"CONF"},"groups":["homegroup"],"config":{"allowedTenants":[{"tenant":{"id":"` + home + `","code":"DBTN"}}]},"profile":{}}`),
			doc(`{"_id":"6a0e000000000000000000b5","username":"outsider","status":"active","tenant":{"id":"` + other + `","code":"CONF"},"groups":[],"config":{},"profile":{}}`),
		},
		groups: []map[string]interface{}{
			doc(`{"code":"owner","tenant":{"id":"` + home + `"},"config":{"allowedPackages":{"DSBRD":["P1","P2"],"OTHER":["O1"]}}}`),
			doc(`{"code":"ops","tenant":{"id":"` + home + `"},"config":{"allowedPackages":{"DSBRD":["P2","P3"]}}}`),
			doc(`{"code":"admin","tenant":{"id":"` + home + `"},"config":{"allowedPackages":{"DSBRD":["ADMIN"]}}}`),
			doc(`{"code":"admin","tenant":{"id":"` + other + `"},"config":{"allowedPackages":{"DSBRD":["WRONG_TENANT"]}}}`),
		},
	}
}

func testDriver(s Store) *Driver { return NewDriverWithStore(s, zap.NewNop()) }

func code(err error) int {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

func TestGetRecord(t *testing.T) {
	s := testStore()
	d := testDriver(s)
	ctx := context.Background()
	tenant := Tenant{ID: home, Code: "DBTN"}

	rec, err := d.GetRecord(ctx, nil, tenant, "owner", ownerID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := rec["password"]; ok {
		t.Error("the password is removed")
	}
	want := map[string]interface{}{"allowedPackages": map[string]interface{}{"DSBRD": []interface{}{"P1", "P2", "P3"}, "OTHER": []interface{}{"O1"}}}
	if !reflect.DeepEqual(rec["groupsConfig"], want) {
		t.Errorf("groupsConfig: %v", rec["groupsConfig"])
	}
	if !reflect.DeepEqual(s.filters[0], bson.M{"username": "owner"}) {
		t.Errorf("looked up by %v", s.filters[0])
	}

	for _, c := range []struct {
		username string
		code     int
	}{{"paused", 406}, {"pending", 405}, {"ghost", 403}, {"outsider", 407}} {
		if _, err := d.GetRecord(ctx, nil, tenant, c.username, "6a0e000000000000000000ff"); code(err) != c.code {
			t.Errorf("%s: got %v, want %d", c.username, err, c.code)
		}
	}

	// an email username is looked up by email
	s.filters = nil
	if _, err := d.GetRecord(ctx, nil, tenant, "owner@x.io", ""); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s.filters[0], bson.M{"email": "owner@x.io"}) {
		t.Errorf("looked up by %v", s.filters[0])
	}

	// without a username, by _id
	s.filters = nil
	if _, err := d.GetRecord(ctx, nil, tenant, "", ownerID); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.filters[0]["_id"]; !ok {
		t.Errorf("looked up by %v", s.filters[0])
	}
	if _, err := d.GetRecord(ctx, nil, tenant, "", "not-an-id"); code(err) != 404 {
		t.Errorf("an invalid id: %v", err)
	}

	s.err = errors.New("db down")
	if _, err := d.GetRecord(ctx, nil, tenant, "owner", ""); code(err) != 400 {
		t.Errorf("a database error: %v", err)
	}
}

// Under a tenant the user is invited to, the user carries that tenant and only its groups.
func TestGetRecordInvitation(t *testing.T) {
	d := testDriver(testStore())
	ctx := context.Background()
	rec, err := d.GetRecord(ctx, nil, Tenant{ID: home, Code: "DBTN"}, "invited", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec["groups"], []interface{}{"admin"}) || rec["tenant"].(map[string]interface{})["id"] != home {
		t.Errorf("groups %v tenant %v", rec["groups"], rec["tenant"])
	}
	want := map[string]interface{}{"allowedPackages": map[string]interface{}{"DSBRD": []interface{}{"ADMIN"}}}
	if !reflect.DeepEqual(rec["groupsConfig"], want) {
		t.Errorf("groupsConfig %v", rec["groupsConfig"])
	}

	rec, err = d.GetRecord(ctx, nil, Tenant{ID: home, Code: "DBTN"}, "invitednogroups", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec["groups"], []interface{}{}) || rec["groupsConfig"] != nil {
		t.Errorf("an invitation without groups grants none: %v %v", rec["groups"], rec["groupsConfig"])
	}
}

func TestGetRecordRoamingAndAutoRoaming(t *testing.T) {
	d := testDriver(testStore())
	ctx := context.Background()
	// roaming from the user's home tenant
	if _, err := d.GetRecord(ctx, nil, Tenant{ID: "6a0e0000000000000000aaaa", Code: "X", Roaming: &Roaming{TID: other}}, "outsider", ""); err != nil {
		t.Errorf("roaming: %v", err)
	}
	reg := &types.RegistryData{CustomRegistry: map[string]interface{}{"urac": map[string]interface{}{"value": map[string]interface{}{
		"autoRoaming": map[string]interface{}{home: map[string]interface{}{other: map[string]interface{}{"groups": []interface{}{"admin"}}}},
	}}}}
	rec, err := d.GetRecord(ctx, reg, Tenant{ID: home, Code: "DBTN"}, "outsider", "")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec["tenant"], map[string]interface{}{"id": home, "code": "DBTN"}) || !reflect.DeepEqual(rec["groups"], []interface{}{"admin"}) {
		t.Errorf("autoRoaming: tenant %v groups %v", rec["tenant"], rec["groups"])
	}
}

func TestMasterDB(t *testing.T) {
	if _, err := MasterDB(&types.RegistryData{}); err == nil {
		t.Error("no masterDB is a configuration error")
	}
	reg := &types.RegistryData{CustomRegistry: map[string]interface{}{"urac": map[string]interface{}{"value": map[string]interface{}{"masterDB": "DBTN_urac"}}}}
	if _, err := MasterDB(reg); err == nil {
		t.Error("a masterDB that is not a core database is a configuration error")
	}
	d := NewDriver(nil, zap.NewNop())
	if _, err := d.GetRecord(context.Background(), reg, Tenant{ID: home}, "owner", ""); code(err) != 400 {
		t.Errorf("got %v", err)
	}
}

// The results were recorded from the soajs.urac.driver pattern in Node.js.
func TestIsEmail(t *testing.T) {
	cases := map[string]bool{
		"a@b.com": true, "a@b.com\n": false, "john": false, "a.b@c-d.co": true, "a-@b.c": true,
		"a@-b.com": false, "a@b-.com": false, "a@[1.2.3.4]": true, "a@b": false, "é@b.com": false,
		"a@b..com": false, "x.y.z@s.example.org": true, "a+b@c.io": true, "a@b.c-": false, "a@bb-b.cc": true,
	}
	for s, want := range cases {
		got, err := IsEmail(s)
		if err != nil || got != want {
			t.Errorf("%q: got %v %v, want %v", s, got, err, want)
		}
	}
}

func pkg(acl map[string]interface{}) *types.Package {
	return &types.Package{ACL: acl, ACLAllEnv: map[string]interface{}{"dev": acl}}
}

func TestMergeACL(t *testing.T) {
	restricted := doc(`{"svc":{"1":{"access":true,"apisPermission":"restricted","get":{"apis":{"/a":{"access":true}}}}}}`)
	restricted2 := doc(`{"svc":{"1":{"access":true,"apisPermission":"restricted","get":{"apis":{"/b":{"access":true}}}}}}`)
	open := doc(`{"svc":{"1":{"access":false,"get":{"apis":{"/c":{"access":true}}}}},"other":{"1":{"access":true}}}`)

	m := MergeACL([]*types.Package{pkg(restricted), pkg(restricted2)})
	apis := m.ACL["svc"].(map[string]interface{})["1"].(map[string]interface{})["get"].(map[string]interface{})["apis"].(map[string]interface{})
	if len(apis) != 2 {
		t.Errorf("two restricted versions merge: %v", apis)
	}
	m = MergeACL([]*types.Package{pkg(restricted), pkg(open)})
	v := m.ACL["svc"].(map[string]interface{})["1"].(map[string]interface{})
	if v["apisPermission"] != "restricted" || v["access"] != true {
		t.Errorf("an open version does not change a restricted one: %v", v)
	}
	if m.ACL["other"] == nil {
		t.Error("a service only in the second package is added")
	}
	m = MergeACL([]*types.Package{pkg(open), pkg(restricted)})
	v = m.ACL["svc"].(map[string]interface{})["1"].(map[string]interface{})
	if v["apisPermission"] != "restricted" || v["get"].(map[string]interface{})["apis"].(map[string]interface{})["/c"] != nil {
		t.Errorf("a restricted version replaces an open one: %v", v)
	}
	if m.ACLAllEnv["dev"].(map[string]interface{})["other"] == nil {
		t.Error("acl_all_env merges too")
	}
	// the packages are not modified
	if len(restricted["svc"].(map[string]interface{})["1"].(map[string]interface{})["get"].(map[string]interface{})["apis"].(map[string]interface{})) != 1 {
		t.Error("a package ACL was modified")
	}
	if MergeACL([]*types.Package{pkg(open)}).ACL == nil {
		t.Error("one package is taken as is")
	}
}

func TestLodashMerge(t *testing.T) {
	got := lodashMerge(doc(`{"a":{"x":1},"l":[{"p":1},2],"n":1}`), doc(`{"a":{"y":2},"l":[{"q":1}],"n":null}`))
	want := doc(`{"a":{"x":1,"y":2},"l":[{"p":1,"q":1},2],"n":null}`)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func tokenOf(user map[string]interface{}, clientID string) *types.AccessToken {
	return &types.AccessToken{ClientID: clientID, Env: "dashboard", User: user}
}

func dbtn() *types.Tenant { return &types.Tenant{ID: home, Code: "DBTN"} }

func packagesOf(acls map[string]map[string]interface{}) func(string) (*types.Package, error) {
	return func(code string) (*types.Package, error) {
		if a, ok := acls[code]; ok {
			return pkg(a), nil
		}
		return nil, &provision.CodeError{Code: 201}
	}
}

func TestUserFromToken(t *testing.T) {
	d := testDriver(testStore())
	oauthUser := doc(`{"id":"` + ownerID + `","username":"owner","loginMode":"oauth","tenant":{"id":"` + home + `","code":"DBTN"}}`)
	u := NewUser(d, &Request{Tenant: dbtn(), Token: tokenOf(oauthUser, home), Logger: zap.NewNop()})
	if rec, err := u.Init(context.Background()); err != nil || rec["loginMode"] != "oauth" {
		t.Errorf("loginMode oauth: the token's user is the record: %v %v", rec, err)
	}

	uracUser := doc(`{"id":"` + ownerID + `","username":"owner","loginMode":"urac","tenant":{"id":"` + home + `","code":"DBTN"},"groupsConfig":{"allowedPackages":{}}}`)
	reg := &types.RegistryData{ServiceConfig: &types.GlobalServiceConfig{}}
	reg.ServiceConfig.OAuth.GetUserFromToken = true
	u = NewUser(d, &Request{Registry: reg, Tenant: dbtn(), Token: tokenOf(uracUser, home), Logger: zap.NewNop()})
	if u.Record() == nil {
		t.Error("getUserFromToken with the token's client tenant: the token's user is the record")
	}
	u = NewUser(d, &Request{Registry: reg, Tenant: dbtn(), Token: tokenOf(uracUser, other), Logger: zap.NewNop()})
	if u.Record() != nil {
		t.Error("getUserFromToken under another tenant reads the record")
	}
	u = NewUser(d, &Request{Tenant: dbtn(), Roaming: &Roaming{TID: other}, Token: tokenOf(uracUser, other), Logger: zap.NewNop()})
	if u.Record() == nil {
		t.Error("roaming: the token's user is the record")
	}

	// read: the record, its groups' packages and the user's key config
	u = NewUser(d, &Request{Tenant: dbtn(), Product: "DSBRD", IKey: "ikey", Token: tokenOf(uracUser, home), Logger: zap.NewNop(),
		Packages: packagesOf(map[string]map[string]interface{}{"P1": doc(`{"svc":{"1":{"access":true}}}`), "P3": doc(`{"x":{"1":{"access":true}}}`)})})
	rec, err := u.Init(context.Background())
	if err != nil || rec["username"] != "owner" {
		t.Fatalf("%v %v", rec, err)
	}
	if acl := u.ACL(); acl["svc"] == nil || acl["x"] == nil {
		t.Errorf("the ACL of P1 and P3, P2 does not exist: %v", acl)
	}
	if !reflect.DeepEqual(u.Config(), doc(`{"svc":{"a":1}}`)) {
		t.Errorf("config %v", u.Config())
	}
	if p := u.Profile(false); p["username"] != "owner" || p["config"] != nil {
		t.Errorf("profile %v", p)
	}

	if _, err := NewUser(d, &Request{Tenant: dbtn(), Token: tokenOf(doc(`{"username":"owner","loginMode":"urac"}`), home), Logger: zap.NewNop()}).Init(context.Background()); err != ErrNoUserID {
		t.Errorf("no user id: %v", err)
	}
}

func TestUserACLPackages(t *testing.T) {
	record := func(codes ...interface{}) *User {
		u := &User{req: &Request{Product: "DSBRD", Logger: zap.NewNop()}}
		u.record = map[string]interface{}{"groupsConfig": map[string]interface{}{"allowedPackages": map[string]interface{}{"DSBRD": codes}}}
		return u
	}
	acls := map[string]map[string]interface{}{"P1": doc(`{"a":{"1":{"access":true}}}`)}

	u := record("MISSING")
	u.req.Packages = packagesOf(acls)
	u.resolveACL()
	if u.ACL() != nil {
		t.Error("one package that does not exist: no ACL")
	}
	u = record("MISSING", "P1")
	u.req.Packages = packagesOf(acls)
	u.resolveACL()
	if u.ACL()["a"] == nil {
		t.Error("with several, a missing package is skipped")
	}
	u = record("P1", "P2")
	u.req.Packages = func(c string) (*types.Package, error) {
		if c == "P2" {
			return nil, errors.New("db down")
		}
		return pkg(acls["P1"]), nil
	}
	u.resolveACL()
	if u.ACL() != nil {
		t.Error("a package that does not load: no ACL")
	}

	// the network package override for users
	u = record("P0")
	u.req.Packages = packagesOf(acls)
	u.req.Registry = &types.RegistryData{CustomRegistry: map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"lastSeen": map[string]interface{}{"network": "net1"}}}}}
	u.req.KeyConfig = doc(`{"gateway":{"networkPackages":{"net1":{"DSBRD":{"users":{"P0":"P1"}}}}}}`)
	u.resolveACL()
	if u.ACL()["a"] == nil {
		t.Error("the network override replaces P0 by P1")
	}
}

func TestResolver(t *testing.T) {
	var got map[string]interface{}
	var inject string
	answer := `{"result":true,"data":{"access":true,"groups":["g1"],"groupsConfig":{"allowedPackages":{"DSBRD":["P1"]}}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		inject = r.Header.Get("soajsinjectobj")
		if r.Method != "POST" || r.URL.Path != "/access" {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(answer))
	}))
	defer srv.Close()
	host, portStr, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portStr)

	tenant := &types.Tenant{ID: home, Code: "DBTN", Profile: map[string]interface{}{"access": map[string]interface{}{"resolver": "clubs", "subject": "club"}}}
	reg := &types.RegistryData{
		Services: map[string]*types.ServiceConfig{"members": {Port: port}},
		CustomRegistry: map[string]interface{}{"urac": map[string]interface{}{"value": map[string]interface{}{
			"tenantAccessResolvers": map[string]interface{}{"clubs": map[string]interface{}{"service": "members", "api": "/access"}},
		}}},
	}
	tokenUser := doc(`{"id":"` + ownerID + `","username":"member","loginMode":"urac","tenant":{"id":"` + other + `","code":"CONF"}}`)
	req := &Request{Registry: reg, Tenant: tenant, Product: "DSBRD", Token: tokenOf(tokenUser, other), Logger: zap.NewNop(),
		Packages: packagesOf(map[string]map[string]interface{}{"P1": doc(`{"svc":{"1":{"access":true}}}`)}),
		Resolve: &ResolveRequest{Registry: reg, Tenant: tenant, Logger: zap.NewNop(),
			Host:      func(string, string) (string, error) { return host, nil },
			InjectObj: InjectObj(tenant, "ik", "ek", "DSBRD", "DSBRD_DEFAULT", "app", doc(`{"commonFields":{"c":1},"members":{"m":1},"other":{}}`), "members"),
		}}
	u := NewUser(testDriver(&fakeStore{}), req)
	if u.resolver == nil {
		t.Fatal("a user switching to a tenant with a resolver")
	}
	rec, err := u.Init(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(rec["groups"], []interface{}{"g1"}) || rec["tenant"].(map[string]interface{})["id"] != home || u.ACL()["svc"] == nil {
		t.Errorf("record %v acl %v", rec, u.ACL())
	}
	wantBody := doc(`{"user":{"id":"` + ownerID + `","username":"member","tenant":{"id":"` + other + `","code":"CONF"}},"tenant":{"id":"` + home + `","code":"DBTN"},"subject":"club"}`)
	if !reflect.DeepEqual(got, wantBody) {
		t.Errorf("body %v", got)
	}
	wantInject := `{"tenant":{"id":"` + home + `","name":"","code":"DBTN","locked":false,"type":""},"key":{"config":{"commonFields":{"c":1},"members":{"m":1}},"iKey":"ik","eKey":"ek"},"application":{"product":"DSBRD","package":"DSBRD_DEFAULT","appId":"app"}}`
	if inject != wantInject {
		t.Errorf("soajsinjectobj\n got  %s\n want %s", inject, wantInject)
	}

	// anything but a well formed yes falls back to the record, which this store does not have
	for _, a := range []string{
		`{"result":true,"data":{"access":false}}`,
		`{"result":false}`,
		`{"result":true,"data":{"access":true,"groupsConfig":{}}}`,
		`not json`,
	} {
		answer = a
		if _, err := NewUser(testDriver(&fakeStore{}), req).Init(context.Background()); code(err) != 403 {
			t.Errorf("%s: %v", a, err)
		}
	}

	// an invitation in the token goes through the record, not the resolver
	invited := copyJSON(tokenUser)
	invited["config"] = map[string]interface{}{"allowedTenants": []interface{}{map[string]interface{}{"tenant": map[string]interface{}{"id": home}}}}
	if NewUser(testDriver(&fakeStore{}), &Request{Registry: reg, Tenant: tenant, Token: tokenOf(invited, other), Logger: zap.NewNop()}).resolver != nil {
		t.Error("an invited user does not use the resolver")
	}
}

func TestResolveMembership(t *testing.T) {
	config := doc(`{"active":true,"AVAPP":["CLUBS","CORPS"]}`)
	memberships := []interface{}{doc(`{"product":"AVAPP","membership":"whale"}`)}
	for product, want := range map[string]string{"AVAPP": "whale", "CLUBS": "whale", "OTHER": ""} {
		if got := ResolveMembership(config, product, memberships); got != want {
			t.Errorf("%s: %q", product, got)
		}
	}
	if ResolveMembership(doc(`{"active":false,"AVAPP":[]}`), "AVAPP", memberships) != "" {
		t.Error("an inactive block resolves nothing")
	}
	if ResolveMembership(config, "AVAPP", []interface{}{}) != "" {
		t.Error("no memberships")
	}
}
