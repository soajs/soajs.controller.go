package provision

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

func newTestProvision() *MongoProvision {
	return &MongoProvision{
		logger:   zap.NewNop(),
		env:      "dev",
		keys:     map[string]*keyEntry{},
		packages: map[string]*packageEntry{},
		oauths:   map[string]*types.TenantOauth{},
		codes:    map[string]string{},
	}
}

func tenantFixture(t *testing.T, expDate string) *tenantDoc {
	t.Helper()
	raw := `{"_id":"6a0e00000000000000000001","code":"CONF","name":"Conf","type":"client","locked":false,"profile":{},
		"tenant":{"id":"6a0e00000000000000000009","code":"MAIN"},
		"oauth":{"secret":"s","type":2,"disabled":0,"loginMode":"urac"},
		"applications":[{"appId":"6a0e000000000000000000a2","product":"CONF","package":"CONF_DEFAULT","_TTL":60000,
		"keys":[{"key":"k1","config":{"dev":{"conform":{"a":1}},"stg":{"x":1}},
		"extKeys":[{"extKey":"e1","env":"DEV","device":null,"geo":null,"expDate":null},
		           {"extKey":"e2","env":"DEV","device":null,"geo":null,"expDate":` + expDate + `}]}]}]}`
	var d tenantDoc
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatal(err)
	}
	return &d
}

func TestKeyData(t *testing.T) {
	p := newTestProvision()
	past := time.Now().Add(-time.Hour).UnixMilli()
	p.ingestTenant(tenantFixture(t, jsonInt(past)), time.Now().UnixMilli())
	entry := p.keys["k1"]
	now := time.Now().UnixMilli()

	k := keyData(entry, "e1", now)
	if k == nil {
		t.Fatal("a listed extKey without expDate resolves")
	}
	if k.Config["conform"] == nil || k.Config["x"] != nil {
		t.Errorf("config must be the key's config for the environment: %v", k.Config)
	}
	if k.Tenant.Main == nil || k.Tenant.Main.Code != "MAIN" {
		t.Error("a sub-tenant carries its main tenant")
	}
	if k.Application.Package != "CONF_DEFAULT" || k.Env != "DEV" {
		t.Errorf("unexpected key %+v", k)
	}
	if keyData(entry, "e2", now) != nil {
		t.Error("an extKey past its expDate must not resolve (S2)")
	}
	if keyData(entry, "unlisted", now) != nil {
		t.Error("an extKey that is not one of the key's extKeys must not resolve (S2)")
	}
	k.Tenant.Code = "CHANGED"
	if keyData(entry, "e1", now).Tenant.Code != "CONF" {
		t.Error("each call returns a new value (S7)")
	}

	p2 := newTestProvision()
	p2.ingestTenant(tenantFixture(t, "0"), now)
	if keyData(p2.keys["k1"], "e2", now) == nil {
		t.Error("an expDate of 0 never expires, as in Node.js")
	}
	if p2.oauths["6a0e00000000000000000001"].Disabled != 0 || p2.codes["6a0e00000000000000000001"] != "CONF" {
		t.Error("tenant oauth and code are cached")
	}
}

// A tenant whose extKey has device and geo as {} decodes through the data schema and its key
// resolves; {} is no restriction, as null.
func TestKeyDataEmptyRestrictions(t *testing.T) {
	doc := `{"_id":{"$oid":"6a0e00000000000000000003"},"code":"CNFE","name":"e","type":"product","console":false,"profile":{},
		"oauth":{"secret":"s","type":2,"disabled":0,"loginMode":"urac"},
		"applications":[{"appId":{"$oid":"6a0e000000000000000000a7"},"product":"CONF","package":"CONF_DEFAULT","_TTL":60000,
		"keys":[{"key":"3d1e2f3a4b5c6d7e8f90a1b2c3d4e5f6","config":{"dev":{"conform":{}}},
		"extKeys":[{"extKey":"5bbf311a","env":"DEV","device":{},"geo":{},"expDate":null}]}]}]}`
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(doc), false, &d); err != nil {
		t.Fatal(err)
	}
	raw, err := bson.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := schemaFor("tenants")
	if err != nil {
		t.Fatal(err)
	}
	var tenant tenantDoc
	if problems, err := schema.Decode(raw, &tenant); err != nil || problems != nil {
		t.Fatalf("device and geo {} must decode: %v %v", problems, err)
	}
	p := newTestProvision()
	now := time.Now().UnixMilli()
	p.ingestTenant(&tenant, now)
	k := keyData(p.keys["3d1e2f3a4b5c6d7e8f90a1b2c3d4e5f6"], "5bbf311a", now)
	if k == nil {
		t.Fatal("the extKey resolves")
	}
	if len(k.Device) != 0 || len(k.Geo) != 0 {
		t.Errorf("device and geo carry no rules: %v %v", k.Device, k.Geo)
	}
}

// Requests change what provision returns (servicesConfig merges, ACL shaping); concurrently, under
// -race, and the cache stays as loaded.
func TestReturnedValuesDoNotShareTheCache(t *testing.T) {
	p := newTestProvision()
	now := time.Now().UnixMilli()
	p.ingestTenant(tenantFixture(t, "null"), now)
	var prod productDoc
	if err := json.Unmarshal([]byte(`{"packages":[{"code":"P_A","_TTL":0,"acl":{"dev":{"svc":{"1":{"access":true,"get":{"apis":{"/a":{"access":true}}}}}}}}]}`), &prod); err != nil {
		t.Fatal(err)
	}
	p.packages = packageEntries(&prod, "dev", "", now)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p.mu.RLock()
			k := keyData(p.keys["k1"], "e1", time.Now().UnixMilli())
			p.mu.RUnlock()
			k.Config["merged"] = i
			k.Config["conform"].(map[string]interface{})["a"] = i
			k.Tenant.Profile["x"] = i
			k.Tenant.Main.Code = "CHANGED"

			pkg, err := p.GetPackageData("P_A")
			if err != nil {
				t.Error(err)
				return
			}
			pkg.ACL["svc"].(map[string]interface{})["1"].(map[string]interface{})["access"] = false
			delete(pkg.ACLAllEnv, "dev")
		}(i)
	}
	wg.Wait()

	k := keyData(p.keys["k1"], "e1", time.Now().UnixMilli())
	if _, ok := k.Config["merged"]; ok || k.Config["conform"].(map[string]interface{})["a"] != float64(1) {
		t.Errorf("the cached key config changed: %v", k.Config)
	}
	if len(k.Tenant.Profile) != 0 || k.Tenant.Main.Code != "MAIN" {
		t.Error("the cached tenant changed")
	}
	pkg, _ := p.GetPackageData("P_A")
	if pkg.ACL["svc"].(map[string]interface{})["1"].(map[string]interface{})["access"] != true || pkg.ACLAllEnv["dev"] == nil {
		t.Error("the cached package ACL changed")
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestPackageEntries(t *testing.T) {
	var prod productDoc
	if err := json.Unmarshal([]byte(`{"packages":[
		{"code":"P_A","_TTL":1000,"acl":{"dev":{"svc":{"1":{"access":true,"get":{"apis":{}}}}},"stg":{"other":{}}}},
		{"code":"P_B","_TTL":0,"acl":{"stg":{}}}]}`), &prod); err != nil {
		t.Fatal(err)
	}
	entries := packageEntries(&prod, "dev", "", 5)
	if len(entries) != 2 {
		t.Fatalf("got %d packages", len(entries))
	}
	a := entries["P_A"].pkg
	if a.ACL["svc"] == nil || a.ACL["other"] != nil || a.ACLAllEnv["stg"] == nil || a.TTL != 1000 {
		t.Errorf("acl must be the environment's, acl_all_env the whole: %+v", a)
	}
	if entries["P_B"].pkg.ACL != nil {
		t.Error("a package without an ACL in the environment has none")
	}
	if only := packageEntries(&prod, "dev", "P_B", 5); len(only) != 1 || only["P_B"] == nil {
		t.Error("a code limits the packages taken")
	}
}
