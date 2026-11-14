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
