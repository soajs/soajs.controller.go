package urac

import (
	"testing"

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
