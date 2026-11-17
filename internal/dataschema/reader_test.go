package dataschema

import (
	"encoding/json"
	"fmt"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

// readDoc reads a document given in extended JSON through a policy.
func readDoc(t *testing.T, coll string, policy Policy, doc string) ([]Finding, bool, map[string]interface{}) {
	t.Helper()
	c := load(t)[coll]
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(doc), false, &d); err != nil {
		t.Fatal(err)
	}
	raw, err := bson.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]interface{}
	findings, ok, err := c.Read(raw, policy, &out)
	if err != nil {
		t.Fatal(err)
	}
	return findings, ok, out
}

func has(findings []Finding, path string, action Action) bool {
	for _, f := range findings {
		if f.Path == path && f.Action == action {
			return true
		}
	}
	return false
}

func dig(v interface{}, path ...interface{}) interface{} {
	for _, p := range path {
		switch k := p.(type) {
		case string:
			m, _ := v.(map[string]interface{})
			v = m[k]
		case int:
			l, _ := v.([]interface{})
			if k >= len(l) {
				return nil
			}
			v = l[k]
		}
	}
	return v
}

func asJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

const tenantBase = `{"_id":{"$oid":"6a0e00000000000000000003"},"code":"T","name":"t","type":"product","oauth":{"secret":"s","type":2,"disabled":0},
	"applications":[{"appId":{"$oid":"6a0e000000000000000000a7"},"product":"P","package":"P_D","_TTL":60000,"keys":[
		{"key":"3d1e2f3a4b5c6d7e8f90a1b2c3d4e5f6","config":{"dev":{}},"extKeys":[%s]}]}%s]}`

const goodExtKey = `{"extKey":"5bbf311a","env":"DEV","device":null,"geo":null,"expDate":null}`

func TestTenantBadExtKeyNextToGoodOne(t *testing.T) {
	doc := fmt.Sprintf(tenantBase, `{"extKey":"5bbf311b","env":"DEV","device":"chrome","geo":null,"expDate":null},`+goodExtKey, "")
	findings, ok, out := readDoc(t, "tenants", Tenants, doc)
	if !ok {
		t.Fatalf("the tenant is read: %v", findings)
	}
	exts := dig(out, "applications", 0, "keys", 0, "extKeys").([]interface{})
	if len(exts) != 1 || dig(exts[0], "extKey") != "5bbf311a" {
		t.Errorf("only the bad extKey is left out: %s", asJSON(exts))
	}
	if !has(findings, "/applications/0/keys/0/extKeys/0/device", DroppedPiece) {
		t.Errorf("findings: %+v", findings)
	}
	for _, f := range findings {
		if f.Action == DroppedPiece && f.Piece != "/applications/0/keys/0/extKeys/0" {
			t.Errorf("piece: %+v", f)
		}
	}
}

func TestTenantUnknownFieldsAndReadings(t *testing.T) {
	doc := `{"_id":{"$oid":"6a0e00000000000000000003"},"code":"T","type":"product","locked":"yes","extra":{"a":1},
		"oauth":{"disabled":"1","pin":{"P":{"enabled":1},"Q":true}},
		"applications":[{"appId":{"$oid":"6a0e000000000000000000a7"},"product":"P","package":"P_D","_TTL":1,"note":"x",
		"keys":[{"key":"3d1e2f3a4b5c6d7e8f90a1b2c3d4e5f6","config":{},"extKeys":[{"extKey":"5bbf311a","env":"DEV","joined":1}]}]}]}`
	findings, ok, out := readDoc(t, "tenants", Tenants, doc)
	if !ok {
		t.Fatalf("read: %v", findings)
	}
	if out["locked"] != true || dig(out, "oauth", "disabled") != 1.0 || dig(out, "oauth", "pin", "P", "enabled") != true || dig(out, "oauth", "pin", "Q") != nil {
		t.Errorf("Node.js readings: %s", asJSON(out))
	}
	for _, path := range []string{"/extra", "/applications/0/note", "/applications/0/keys/0/extKeys/0/joined"} {
		if !has(findings, path, IgnoredField) {
			t.Errorf("%s is ignored: %+v", path, findings)
		}
	}
	if len(dig(out, "applications", 0, "keys", 0, "extKeys").([]interface{})) != 1 {
		t.Error("an extKey with an unknown field and no device or geo is kept: missing means no restriction")
	}
}

func TestTenantApplicationACLIsOldShape(t *testing.T) {
	doc := fmt.Sprintf(tenantBase, goodExtKey, `,{"appId":{"$oid":"6a0e000000000000000000a8"},"product":"P","package":"P_D","_TTL":1,"acl":{"svc":{}},"keys":[]}`)
	findings, ok, out := readDoc(t, "tenants", Tenants, doc)
	if !ok || len(out["applications"].([]interface{})) != 1 || !has(findings, "/applications/1", OldShape) {
		t.Errorf("an application with its own ACL is left out as an old shape: %v %+v", ok, findings)
	}
}

func TestTenantSkippedOnlyForIdentity(t *testing.T) {
	for _, doc := range []string{
		`{"_id":{"$oid":"6a0e00000000000000000003"},"code":"t","type":"product"}`,
		`{"_id":{"$oid":"6a0e00000000000000000003"},"code":"T","type":"reseller"}`,
		`{"_id":{"$oid":"6a0e00000000000000000003"},"type":"product"}`,
	} {
		if findings, ok, _ := readDoc(t, "tenants", Tenants, doc); ok || !has(findings, findings[len(findings)-1].Path, SkippedDocument) {
			t.Errorf("%s must be skipped: %+v", doc, findings)
		}
	}
	// everything else broken: the tenant is still read, without the broken pieces
	doc := `{"_id":{"$oid":"6a0e00000000000000000003"},"code":"T","type":"client","name":5,"profile":"p","tenant":{"id":"x"},
		"oauth":{"disabled":0,"pin":{"P":{"enabled":true,"x":1}}},"applications":[{"product":"P"}]}`
	findings, ok, out := readDoc(t, "tenants", Tenants, doc)
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	if out["name"] != nil || out["profile"] != nil || out["tenant"] != nil || len(out["applications"].([]interface{})) != 0 {
		t.Errorf("the broken pieces are left out: %s", asJSON(out))
	}
}

const productBase = `{"_id":{"$oid":"6a0e000000000000000000b1"},"code":"P","name":"p","console":false,"packages":[
	{"code":"P_D","name":"d","_TTL":1,"aclTypeByEnv":{"dev":"granular"},"acl":{"dev":{%s}}}]}`

func TestProductRestrictedRuleDropped(t *testing.T) {
	acl := `"svc":{"1":{"access":true,"apisPermission":"restricted","get":{"apis":{"/a":{"access":true},"/b":{"access":true,"group":7}}}}},
		"other":{"1":{"access":false,"get":{"apis":{"/c":{"access":false}}}}}`
	findings, ok, out := readDoc(t, "products", Products, fmtDoc(productBase, acl))
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	apis := dig(out, "packages", 0, "acl", "dev", "svc", "1", "get", "apis").(map[string]interface{})
	if apis["/a"] == nil || apis["/b"] != nil || dig(out, "packages", 0, "acl", "dev", "other") == nil {
		t.Errorf("under restricted only the broken rule is left out: %s", asJSON(out))
	}
}

// Without restricted, leaving out a rule would open the API: the whole service is left out.
func TestProductUnrestrictedRuleDropsService(t *testing.T) {
	acl := `"svc":{"1":{"access":false,"get":{"apis":{"/a":{"access":true},"/b":{"access":true,"group":7}}}}},
		"other":{"1":{"access":false,"get":{"apis":{"/c":{"access":false}}}}}`
	findings, ok, out := readDoc(t, "products", Products, fmtDoc(productBase, acl))
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	if dig(out, "packages", 0, "acl", "dev", "svc") != nil || dig(out, "packages", 0, "acl", "dev", "other") == nil {
		t.Errorf("the service is left out, the others kept: %s", asJSON(out))
	}
	found := false
	for _, f := range findings {
		if f.Action == DroppedPiece && f.Piece == "/packages/0/acl/dev/svc" {
			found = true
		}
	}
	if !found {
		t.Errorf("findings: %+v", findings)
	}
}

func TestProductNodeReadings(t *testing.T) {
	acl := `"svc":{"1":{"get":{"apis":{"/a":{},"/b":{"access":"yes"}}},"post":null,"put":{},"apisPermission":"open"},
		"2":{"access":1}}`
	findings, ok, out := readDoc(t, "products", Products, fmtDoc(productBase, acl))
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	v1 := dig(out, "packages", 0, "acl", "dev", "svc", "1")
	if dig(v1, "access") != false || dig(v1, "get", "apis", "/a", "access") != false || dig(v1, "get", "apis", "/b", "access") != true {
		t.Errorf("access readings: %s", asJSON(v1))
	}
	if dig(v1, "post") != nil || asJSON(dig(v1, "put", "apis")) != "{}" || dig(v1, "apisPermission") != nil {
		t.Errorf("method and apisPermission readings: %s", asJSON(v1))
	}
	if dig(out, "packages", 0, "acl", "dev", "svc", "2", "access") != true {
		t.Errorf("a version without methods is read: %s", asJSON(out))
	}
	for _, f := range findings {
		if f.Action != ReadAsNode {
			t.Errorf("only readings: %+v", f)
		}
		if f.Rule == "" {
			t.Errorf("each reading cites Node.js: %+v", f)
		}
	}
}

func TestProductOldShapes(t *testing.T) {
	for name, acl := range map[string]string{
		"service-level apis":        `"svc":{"apis":{"/a":{"access":true}}}`,
		"service-level access":      `"svc":{"access":true}`,
		"version-level apis":        `"svc":{"1":{"access":true,"apis":{"/a":{"access":true}}}}`,
		"method-level apisRegExp":   `"svc":{"1":{"access":true,"get":{"apis":{},"apisRegExp":[]}}}`,
		"method apisPermission":     `"svc":{"1":{"access":true,"get":{"apis":{},"apisPermission":"restricted"}}}`,
		"version access as groups":  `"svc":{"1":{"access":["admin"],"get":{"apis":{}}}}`,
		"api access as groups open": `"svc":{"1":{"access":false,"get":{"apis":{"/a":{"access":["admin"]}}}}}`,
	} {
		findings, ok, out := readDoc(t, "products", Products, fmtDoc(productBase, acl+`,"other":{"1":{"access":true}}`))
		if !ok || dig(out, "packages", 0, "acl", "dev", "svc") != nil || dig(out, "packages", 0, "acl", "dev", "other") == nil ||
			!has(findings, "/packages/0/acl/dev/svc", OldShape) {
			t.Errorf("%s: the service is left out as an old shape: %v %+v", name, ok, findings)
		}
	}
	// under restricted, an API rule with groups is left out alone
	findings, ok, out := readDoc(t, "products", Products, fmtDoc(productBase,
		`"svc":{"1":{"access":true,"apisPermission":"restricted","get":{"apis":{"/a":{"access":["admin"]},"/b":{"access":true}}}}}`))
	if !ok || dig(out, "packages", 0, "acl", "dev", "svc", "1", "get", "apis", "/a") != nil ||
		dig(out, "packages", 0, "acl", "dev", "svc", "1", "get", "apis", "/b") == nil || !has(findings, "/packages/0/acl/dev/svc/1/get/apis/~1a", OldShape) {
		t.Errorf("restricted: %v %+v", ok, findings)
	}
}

func TestProductUnits(t *testing.T) {
	doc := `{"_id":{"$oid":"6a0e000000000000000000b1"},"code":"P","packages":[
		{"code":"P_A","_TTL":"x","acl":{}},
		{"code":"P_B","acl":"none"},
		{"code":"P_C","name":5,"acl":{"DEV":{},"dev":{"svc":{"1x0":{"access":true}},"ok":{"1":{"access":true}}}}}]}`
	findings, ok, out := readDoc(t, "products", Products, doc)
	if !ok {
		t.Fatalf("read: %+v", findings)
	}
	pkgs := out["packages"].([]interface{})
	if len(pkgs) != 1 || dig(pkgs[0], "code") != "P_C" || dig(pkgs[0], "acl", "DEV") != nil ||
		dig(pkgs[0], "acl", "dev", "svc") != nil || dig(pkgs[0], "acl", "dev", "ok") == nil {
		t.Errorf("units: %s", asJSON(out))
	}
	if findings, ok, _ := readDoc(t, "products", Products, `{"_id":{"$oid":"6a0e000000000000000000b1"},"code":"P"}`); ok {
		t.Errorf("no packages list: skipped, %+v", findings)
	}
}
