package dataschema

import (
	"os"
	"path/filepath"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func load(t *testing.T) map[string]*Collection {
	t.Helper()
	collections, err := Load(DefaultDir())
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]*Collection{}
	for _, c := range collections {
		if byName[c.Name] != nil {
			t.Fatalf("two schemas for %s", c.Name)
		}
		byName[c.Name] = c
	}
	return byName
}

func valid(t *testing.T, c *Collection, doc string) []Problem {
	t.Helper()
	problems, err := c.ValidateJSON([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return problems
}

func TestSchemasCompile(t *testing.T) {
	byName := load(t)
	for _, name := range []string{"environment", "infra", "resources", "custom_registry", "marketplace", "hosts",
		"controllers", "products", "tenants", "oauth_token", "users", "groups", "throttling_monitor",
		"throttling_error", "idempotency_store", "cache_store", "cache_invalidation"} {
		if byName[name] == nil {
			t.Errorf("no schema for %s", name)
		}
	}
}

// A cluster without authentication and a cluster with credentials are the same shape.
func TestResourceClusterCredentials(t *testing.T) {
	c := load(t)["resources"]
	base := `{"_id":{"$oid":"6a0e000000000000000000c1"},"name":"c","type":"cluster","category":"mongo","created":"DEV",
		"plugged":true,"shared":false,"sharedEnvs":{},"config":{"protocol":"mongodb://","servers":[{"host":"127.0.0.1","port":27017}],"URLParam":{}%s}}`
	if p := valid(t, c, fmtDoc(base, "")); p != nil {
		t.Errorf("cluster without credentials: %v", p)
	}
	if p := valid(t, c, fmtDoc(base, `,"credentials":{"username":"u","password":"p"}`)); p != nil {
		t.Errorf("cluster with credentials: %v", p)
	}
	if p := valid(t, c, fmtDoc(base, `,"credentials":{"username":""}`)); p == nil {
		t.Error("credentials without a password must not validate")
	}
}

// Shapes Node.js still reads for compatibility are refused.
func TestOldShapesRefused(t *testing.T) {
	byName := load(t)
	tests := []struct {
		collection, name, doc string
	}{
		{"products", "version-less service ACL",
			`{"_id":{"$oid":"6a0e000000000000000000a1"},"code":"P","name":"p","console":false,"packages":[{"code":"P_D","name":"d","_TTL":1,
			"aclTypeByEnv":{"dev":"granular"},"acl":{"dev":{"svc":{"access":true,"apis":{"/a":{"access":true}}}}}}]}`},
		{"products", "aclType instead of aclTypeByEnv",
			`{"_id":{"$oid":"6a0e000000000000000000a1"},"code":"P","name":"p","console":false,"packages":[{"code":"P_D","name":"d","_TTL":1,
			"aclType":"granular","acl":{"dev":{}}}]}`},
		{"environment", "dbs.clusters",
			`{"_id":{"$oid":"6a0e000000000000000000e1"},"code":"DEV","domain":"d","protocol":"http","port":80,
			"deployer":{"type":"manual","selected":"manual"},"dbs":{"config":{"prefix":""},"databases":{},"clusters":{}},"services":{}}`},
		{"oauth_token", "oauth_urac user",
			`{"_id":{"$oid":"6a0e000000000000000000ee"},"token":"c0f0000000000000000000000000000000000001","type":"accessToken",
			"clientId":"x","env":"dev","expires":{"$date":"2099-01-01T00:00:00Z"},"user":{"userId":"u","tId":"t"}}`},
		{"custom_registry", "urac dbCodes",
			`{"_id":{"$oid":"6a0e000000000000000000d1"},"name":"urac","created":"DEV","plugged":true,"shared":false,"sharedEnvs":{},
			"value":{"masterDB":"og","dbCodes":{}}}`},
	}
	for _, tt := range tests {
		if p := valid(t, byName[tt.collection], tt.doc); p == nil {
			t.Errorf("%s: %s must not validate", tt.collection, tt.name)
		}
	}
}

// An extKey's device and geo may be null, {} or allow/deny lists; {} is what soajs.multitenant
// writes for no restriction.
func TestTenantExtKeyRestrictions(t *testing.T) {
	c := load(t)["tenants"]
	base := `{"_id":{"$oid":"6a0e00000000000000000003"},"code":"T","name":"t","type":"product","console":false,"profile":{},
		"oauth":{"secret":"s","type":2,"disabled":0,"loginMode":"urac"},
		"applications":[{"appId":{"$oid":"6a0e000000000000000000a7"},"product":"P","package":"P_D","_TTL":60000,
		"keys":[{"key":"3d1e2f3a4b5c6d7e8f90a1b2c3d4e5f6","config":{},"extKeys":[{"extKey":"5bbf311a","env":"DEV","expDate":null,%s}]}]}]}`
	for _, ok := range []string{
		`"device":null,"geo":null`,
		`"device":{},"geo":{}`,
		`"device":{"allow":[{"family":"Chrome"}]},"geo":{"deny":["10.0.0.0/8"]}`,
	} {
		if p := valid(t, c, fmtDoc(base, ok)); p != nil {
			t.Errorf("%s: %v", ok, p)
		}
	}
	for _, bad := range []string{
		`"device":{"block":[]},"geo":null`,
		`"device":null,"geo":{"block":[]}`,
		`"device":[],"geo":null`,
	} {
		if p := valid(t, c, fmtDoc(base, bad)); p == nil {
			t.Errorf("%s must not validate", bad)
		}
	}
}

// Users and groups are extended by the services that own them (Connect writes lastVisit, joinedAt
// and via in an invitation): fields the gateway does not read are accepted, the ones it reads are
// still checked.
func TestUracUnknownFields(t *testing.T) {
	byName := load(t)
	user := `{"_id":{"$oid":"6a0e000000000000000000b7"},"username":"u","status":"active",
		"tenant":{"id":"6a0e00000000000000000001","code":"CONF","extra":1},"groups":[],"profile":{},%s}`
	for _, ok := range []string{
		`"config":{"allowedTenants":[{"tenant":{"id":"6a0e00000000000000000002","code":"DBTN"},"groups":["admin"],` +
			`"lastVisit":1794666000000,"joinedAt":1794666000000,"via":{"tenantId":"6a0e00000000000000000001"}}]}`,
		`"config":{"other":true},"preferences":{"theme":"dark"}`,
		`"config":{},"lastLogin":{"$date":"2026-11-17T00:00:00Z"},"locked":"no","ts":"x"`,
		`"config":{},"memberships":[{"product":"P","membership":"m","since":1}]`,
	} {
		if p := valid(t, byName["users"], fmtDoc(user, ok)); p != nil {
			t.Errorf("users %s: %v", ok, p)
		}
	}
	for _, bad := range []string{
		`"config":{"allowedTenants":[{"tenant":{"id":"6a0e00000000000000000002"},"lastVisit":1}]}`,
		`"config":{"allowedTenants":[{"groups":["admin"],"lastVisit":1}]}`,
		`"config":{"allowedTenants":[{"tenant":{"id":"6a0e00000000000000000002","code":"DBTN"},"groups":"admin"}]}`,
	} {
		if p := valid(t, byName["users"], fmtDoc(user, bad)); p == nil {
			t.Errorf("users %s must not validate", bad)
		}
	}
	group := `{"_id":{"$oid":"6a0e000000000000000000c7"},"code":"admin","tenant":{"id":"6a0e00000000000000000002","code":"DBTN","extra":1},%s}`
	if p := valid(t, byName["groups"], fmtDoc(group, `"config":{"allowedPackages":{"DSBRD":["DSBRD_DEFAULT"]},"other":1},"chat":true`)); p != nil {
		t.Errorf("groups with extra fields: %v", p)
	}
	if p := valid(t, byName["groups"], fmtDoc(group, `"config":{"allowedPackages":{"DSBRD":"DSBRD_DEFAULT"}}`)); p == nil {
		t.Error("allowedPackages must stay a map of lists")
	}
}

// The conformance fixture, when present, follows the schema.
func TestConformanceFixture(t *testing.T) {
	dir := filepath.Join(DefaultDir(), "..", "..", "test", "conformance", "fixture")
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) == 0 {
		t.Skip("no conformance fixture in this tree")
	}
	byName := load(t)
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var collections bson.D
		if err := bson.UnmarshalExtJSON(raw, false, &collections); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, coll := range collections {
			c := byName[coll.Key]
			if c == nil {
				t.Errorf("%s: no schema for %s", filepath.Base(f), coll.Key)
				continue
			}
			for i, doc := range coll.Value.(bson.A) {
				raw, err := bson.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				problems, err := c.ValidateBSON(raw)
				if err != nil {
					t.Fatal(err)
				}
				if problems != nil {
					t.Errorf("%s %s[%d]: %v", filepath.Base(f), coll.Key, i, problems)
				}
			}
		}
	}
}

func fmtDoc(format, extra string) string {
	out := ""
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) && format[i+1] == 's' {
			out += extra
			i++
			continue
		}
		out += string(format[i])
	}
	return out
}
