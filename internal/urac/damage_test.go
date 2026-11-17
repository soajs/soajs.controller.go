package urac

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"

	"github.com/soajs/soajs.controller.go/internal/dataschema"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

func restrictedACL(apis ...string) map[string]interface{} {
	list := map[string]interface{}{}
	for _, a := range apis {
		list[a] = map[string]interface{}{"access": true}
	}
	return map[string]interface{}{"1": map[string]interface{}{"access": true, "apisPermission": "restricted",
		"get": map[string]interface{}{"apis": list}}}
}

func openACL() map[string]interface{} {
	return map[string]interface{}{"1": map[string]interface{}{"access": false}}
}

// A package the gateway could not read refuses everything: the user is not given the key's
// package ACL, as for a package that does not exist.
func TestDamageWholePackageRefuses(t *testing.T) {
	broken := &types.Package{Code: "P_BROKEN", ACL: map[string]interface{}{}, ACLAllEnv: map[string]interface{}{},
		Damage: &types.PackageDamage{Whole: true}}
	good := &types.Package{Code: "P_GOOD", ACL: map[string]interface{}{"svc": openACL()},
		ACLAllEnv: map[string]interface{}{"dev": map[string]interface{}{"svc": openACL()}}}
	for name, pkgs := range map[string][]*types.Package{"only package": {broken}, "one of several": {good, broken}} {
		acl := applyDamage(MergeACL(pkgs), pkgs, "dev")
		if acl == nil || acl.ACL == nil || len(acl.ACL) != 0 || len(acl.ACLAllEnv) != 0 {
			t.Errorf("%s: an empty ACL, not none: %+v", name, acl)
		}
	}
	envOnly := &types.Package{Code: "P_ENV", ACL: nil, Damage: &types.PackageDamage{Envs: map[string]bool{"dev": true}}}
	if acl := applyDamage(MergeACL([]*types.Package{good, envOnly}), []*types.Package{good, envOnly}, "dev"); acl == nil || len(acl.ACL) != 0 {
		t.Errorf("a package whose ACL for the environment was left out refuses everything: %+v", acl)
	}
}

// A service left out of one package is removed from the merged ACL even when another package has
// it: merged, the other package's open version would replace nothing, but the left-out one may
// have been restricted.
func TestDamageServiceRemovedFromMerge(t *testing.T) {
	a := &types.Package{Code: "P_A", ACL: map[string]interface{}{"other": openACL()},
		ACLAllEnv: map[string]interface{}{"dev": map[string]interface{}{"other": openACL()}},
		Damage:    &types.PackageDamage{Services: map[string]map[string]bool{"dev": {"svc": true}}}}
	b := &types.Package{Code: "P_B", ACL: map[string]interface{}{"svc": openACL(), "third": restrictedACL("/x")},
		ACLAllEnv: map[string]interface{}{"dev": map[string]interface{}{"svc": openACL(), "third": restrictedACL("/x")}}}
	pkgs := []*types.Package{a, b}
	acl := applyDamage(MergeACL(pkgs), pkgs, "dev")
	if acl.ACL["svc"] != nil || acl.ACL["other"] == nil || acl.ACL["third"] == nil {
		t.Errorf("svc is removed, the rest kept: %v", acl.ACL)
	}
	if byEnv, _ := acl.ACLAllEnv["dev"].(map[string]interface{}); byEnv["svc"] != nil || byEnv["other"] == nil {
		t.Errorf("acl_AllEnv too: %v", acl.ACLAllEnv)
	}
	// without damage, the merge is unchanged
	clean := []*types.Package{{Code: "P_A", ACL: map[string]interface{}{"svc": restrictedACL("/a")}}, b}
	if acl := applyDamage(MergeACL(clean), clean, "dev"); acl.ACL["svc"] == nil {
		t.Errorf("no damage, no change: %v", acl.ACL)
	}
}

// A user's roles that cannot be read give no packages, as Node.js: the key's package ACL applies
// when nothing else gives one, a readable group next to them still gives its packages, and each is
// reported as falling back to the key's package.
func TestUnreadableRolesFallBack(t *testing.T) {
	saved := dataschema.Problems
	defer func() { dataschema.Problems = saved }()
	dataschema.Problems = dataschema.NewStore()
	s := &mongoStore{logger: zap.NewNop()}
	doc := func(ext string) bson.Raw {
		t.Helper()
		var d bson.D
		if err := bson.UnmarshalExtJSON([]byte(ext), false, &d); err != nil {
			t.Fatal(err)
		}
		raw, _ := bson.Marshal(d)
		return raw
	}
	broken, err := s.read("groups", dataschema.Groups, doc(`{"_id":{"$oid":"6a0e000000000000000000c8"},"code":"b","tenant":{"id":"6a0e00000000000000000002","code":"DBTN"},"config":{"allowedPackages":["X"]}}`))
	if err != nil || broken != nil {
		t.Fatalf("a group that cannot be read gives nothing: %v %v", broken, err)
	}
	partly, _ := s.read("groups", dataschema.Groups, doc(`{"_id":{"$oid":"6a0e000000000000000000c9"},"code":"p","tenant":{"id":"6a0e00000000000000000002","code":"DBTN"},"config":{"allowedPackages":{"DSBRD":"DSBRD_ADMIN","OTHER":["OTHER_D"]}}}`))
	good, _ := s.read("groups", dataschema.Groups, doc(`{"_id":{"$oid":"6a0e000000000000000000ca"},"code":"g","tenant":{"id":"6a0e00000000000000000002","code":"DBTN"},"config":{"allowedPackages":{"DSBRD":["DSBRD_ADMIN"]}}}`))

	only := map[string]interface{}{"groupsConfig": []map[string]interface{}{partly}}
	assureConfig(only)
	u := &User{req: &Request{Product: "DSBRD"}, record: only}
	if u.AllowedPackages() != nil {
		t.Errorf("no packages under DSBRD, so the key's package ACL applies: %v", u.AllowedPackages())
	}
	both := map[string]interface{}{"groupsConfig": []map[string]interface{}{partly, good}}
	assureConfig(both)
	u = &User{req: &Request{Product: "DSBRD"}, record: both}
	// as Node.js: the string's characters merged with the readable group's list; the characters
	// are packages that do not exist, skipped, so the readable group's package gives the ACL
	if got := u.AllowedPackages(); len(got) != 10 || got[0] != "D" || got[9] != "DSBRD_ADMIN" {
		t.Errorf("the readable group's packages, as Node.js merges them: %v", got)
	}
	user, err := s.read("users", dataschema.Users, doc(`{"_id":{"$oid":"6a0e000000000000000000cb"},"username":"u","status":"active",
		"tenant":{"id":"6a0e00000000000000000002","code":"DBTN"},"groups":"admin"}`))
	if err != nil || user == nil || user["groups"] != nil {
		t.Errorf("a user whose groups cannot be read is read without them: %v %v", user, err)
	}
	fell := map[string]bool{}
	for _, f := range dataschema.Problems.List() {
		if f.Action == dataschema.FellBackToKeyPackage {
			fell[f.Collection+" "+f.ID] = true
		}
	}
	for _, want := range []string{"groups 6a0e000000000000000000c8", "groups 6a0e000000000000000000c9", "users 6a0e000000000000000000cb"} {
		if !fell[want] {
			t.Errorf("%s is reported as falling back: %v", want, dataschema.Problems.List())
		}
	}
	if fell["groups 6a0e000000000000000000ca"] {
		t.Error("a readable group is not reported")
	}
}
