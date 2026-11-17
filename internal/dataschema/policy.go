package dataschema

import (
	"sort"
	"strconv"
)

// The policies say, for each collection, how a document that does not match its schema is read
// (docs/data-schema/imperfect-data.md): the Node.js readings first, then the smallest piece that
// holds a problem is left out, toward refusal, never toward access.

// policyFuncs builds a Policy from functions.
type policyFuncs struct {
	prepare func(r *Reading)
	unit    func(r *Reading, path []string) []string
	closed  func(path []string) bool
}

func (p policyFuncs) Prepare(r *Reading) {
	if p.prepare != nil {
		p.prepare(r)
	}
}

func (p policyFuncs) Unit(r *Reading, path []string) []string {
	if p.unit == nil {
		return nil
	}
	return p.unit(r, path)
}

func (p policyFuncs) Closed(path []string) bool {
	return p.closed != nil && p.closed(path)
}

// WholeDocument reads a document whole or not at all; fields the spec does not list are ignored.
var WholeDocument Policy = policyFuncs{}

func cut(path []string, n int) []string {
	if len(path) < n {
		n = len(path)
	}
	return append([]string{}, path[:n]...)
}

func p(parts ...string) []string { return parts }

// indexesDesc are an array's indexes, highest first, so leaving one out keeps the others'.
func indexesDesc(list []interface{}) []int {
	out := make([]int, len(list))
	for i := range list {
		out[i] = len(list) - 1 - i
	}
	return out
}

func keysOf(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func with(path []string, more ...string) []string {
	return append(append([]string{}, path...), more...)
}

// ---------------------------------------------------------------------------------------------
// tenants

// Tenants: the tenant is skipped only when its _id, code or type cannot be read; otherwise an
// extKey, then a key, then an application is left out.
var Tenants Policy = policyFuncs{prepare: prepareTenant, unit: tenantUnit}

func prepareTenant(r *Reading) {
	r.ReadBool(p("locked"), "soajs.controller mw/mt/index.js:218 (injected; JavaScript truthiness)")
	if v, ok := r.Get("oauth", "disabled"); ok {
		want := 0.0
		if Truthy(v) {
			want = 1
		}
		if f, isNum := v.(float64); !isNum || f != want {
			r.Set(p("oauth", "disabled"), want)
			r.ReadAs(p("oauth", "disabled"), "soajs.controller mw/mt/utils.js:559 (tenantOauth.disabled, truthiness)")
		}
	}
	if pin, ok := r.Get("oauth", "pin"); ok {
		const ref = "soajs.controller mw/mt/utils.js:629 (pin[product] && pin[product].enabled)"
		if m, isMap := pin.(map[string]interface{}); !isMap {
			r.Drop(p("oauth", "pin"), ref, ReadAsNode)
		} else {
			for _, product := range keysOf(m) {
				entry, isEntry := m[product].(map[string]interface{})
				if !isEntry {
					r.Drop(p("oauth", "pin", product), ref, ReadAsNode)
					continue
				}
				if _, has := entry["enabled"]; !has {
					entry["enabled"] = false
					r.ReadAs(p("oauth", "pin", product, "enabled"), ref)
					continue
				}
				r.ReadBool(p("oauth", "pin", product, "enabled"), ref)
			}
		}
	}
	apps, _ := r.Get("applications")
	if list, isList := apps.([]interface{}); isList {
		for _, i := range indexesDesc(list) {
			app, _ := list[i].(map[string]interface{})
			for _, old := range []string{"acl", "acl_all_env"} {
				if _, has := app[old]; has {
					// an application ACL overrides the package's (mw/keyACL/index.js:26-29)
					r.Drop(p("applications", strconv.Itoa(i)), "soajs.controller mw/keyACL/index.js:26-29 (application "+old+")", OldShape)
					break
				}
			}
		}
	}
}

func tenantUnit(r *Reading, path []string) []string {
	if len(path) == 0 {
		return nil
	}
	switch path[0] {
	case "_id", "code", "type":
		return nil
	case "applications":
		if len(path) <= 2 {
			return cut(path, len(path))
		}
		switch path[2] {
		case "description":
			return cut(path, 3)
		case "keys":
			if len(path) <= 4 {
				return cut(path, len(path))
			}
			if path[4] != "extKeys" {
				return cut(path, 4)
			}
			if len(path) <= 6 {
				return cut(path, len(path))
			}
			if path[6] == "label" || path[6] == "dashboardAccess" {
				return cut(path, 7)
			}
			return cut(path, 6)
		}
		return cut(path, 2)
	case "profile":
		if len(path) >= 2 && path[1] == "access" {
			return cut(path, 2)
		}
		return cut(path, 1)
	}
	return cut(path, 1)
}

// ---------------------------------------------------------------------------------------------
// products

var aclMethods = []string{"get", "post", "put", "delete", "patch", "head", "options"}

// Products: under an apisPermission "restricted" version a broken API rule is left out (the API
// is then refused, 159); anything else unreadable in a service's ACL leaves out that service for
// the package and environment (154); an unreadable package is left out; the product is skipped
// only when its _id or code cannot be read, or it has no packages list.
var Products Policy = policyFuncs{prepare: prepareProduct, unit: productUnit}

func prepareProduct(r *Reading) {
	pkgs, _ := r.Get("packages")
	list, isList := pkgs.([]interface{})
	if !isList {
		return
	}
	for i := range list {
		pi := strconv.Itoa(i)
		acl, _ := r.Get("packages", pi, "acl")
		envs, isMap := acl.(map[string]interface{})
		if !isMap {
			continue
		}
		for _, env := range keysOf(envs) {
			services, isServices := envs[env].(map[string]interface{})
			if !isServices {
				continue
			}
			for _, svc := range keysOf(services) {
				prepareServiceACL(r, p("packages", pi, "acl", env, svc))
			}
		}
	}
}

// prepareServiceACL applies the Node.js readings to one service's ACL and leaves it out when it
// carries an old shape.
func prepareServiceACL(r *Reading, svcPath []string) {
	v, _ := r.Get(svcPath...)
	svc, isMap := v.(map[string]interface{})
	if !isMap {
		return
	}
	for _, old := range []string{"access", "apis", "apisRegExp", "apisPermission"} {
		if _, has := svc[old]; has {
			r.Drop(svcPath, "soajs.controller mw/keyACL/index.js:34-44 (an ACL without versions, "+old+")", OldShape)
			return
		}
	}
	for _, ver := range keysOf(svc) {
		version, isVersion := svc[ver].(map[string]interface{})
		if !isVersion {
			continue
		}
		vPath := with(svcPath, ver)
		for _, old := range []string{"apis", "apisRegExp"} {
			if _, has := version[old]; has {
				r.Drop(svcPath, "soajs.controller mw/keyACL/index.js:97 (version-level "+old+")", OldShape)
				return
			}
		}
		const accessRef = "soajs.controller mw/mt/utils.js:848 (system.access, truthiness)"
		switch a := version["access"].(type) {
		case nil:
			version["access"] = false
			r.ReadAs(with(vPath, "access"), accessRef)
		case bool:
		case []interface{}:
			r.Drop(svcPath, "soajs.controller mw/mt/utils.js:853-863 (version access as a list of groups)", OldShape)
			return
		default:
			version["access"] = Truthy(a)
			r.ReadAs(with(vPath, "access"), accessRef)
		}
		if perm, has := version["apisPermission"]; has && perm != "restricted" {
			delete(version, "apisPermission")
			r.ReadAs(with(vPath, "apisPermission"), "soajs.controller mw/mt/utils.js:214, 883 (only \"restricted\" restricts)")
		}
		restricted := version["apisPermission"] == "restricted"
		for _, m := range aclMethods {
			mv, has := version[m]
			if !has {
				continue
			}
			mPath := with(vPath, m)
			if !Truthy(mv) {
				delete(version, m)
				r.ReadAs(mPath, "soajs.controller mw/keyACL/index.js:74 (a method that is not an object is not used)")
				continue
			}
			method, isMethod := mv.(map[string]interface{})
			if !isMethod {
				continue
			}
			for _, old := range []string{"apisPermission", "apisRegExp"} {
				if _, has := method[old]; has {
					r.Drop(svcPath, "soajs.controller mw/keyACL/index.js:85-92 (method-level "+old+")", OldShape)
					return
				}
			}
			if !Truthy(method["apis"]) {
				method["apis"] = map[string]interface{}{}
				r.ReadAs(with(mPath, "apis"), "soajs.controller mw/keyACL/index.js:79 (a method without apis has no API rules)")
			}
			apis, isAPIs := method["apis"].(map[string]interface{})
			if !isAPIs {
				continue
			}
			for _, route := range keysOf(apis) {
				rule, isRule := apis[route].(map[string]interface{})
				if !isRule {
					continue
				}
				rPath := with(mPath, "apis", route)
				const ruleRef = "soajs.controller mw/mt/utils.js:225-228 (api.access, truthiness)"
				switch a := rule["access"].(type) {
				case nil:
					rule["access"] = false
					r.ReadAs(with(rPath, "access"), ruleRef)
				case bool:
				case []interface{}:
					if restricted {
						r.Drop(rPath, "soajs.controller mw/mt/utils.js:232-243 (API access as a list of groups)", OldShape)
						continue
					}
					r.Drop(svcPath, "soajs.controller mw/mt/utils.js:232-243 (API access as a list of groups)", OldShape)
					return
				default:
					rule["access"] = Truthy(a)
					r.ReadAs(with(rPath, "access"), ruleRef)
				}
			}
		}
	}
}

func productUnit(r *Reading, path []string) []string {
	if len(path) == 0 {
		return nil
	}
	switch path[0] {
	case "_id", "code":
		return nil
	case "packages":
		if len(path) == 1 {
			return nil
		}
		if len(path) == 2 {
			return cut(path, 2)
		}
		switch path[2] {
		case "name", "description", "locked", "tags", "aclTypeByEnv":
			return cut(path, 3)
		case "acl":
			if len(path) == 3 {
				// an acl that is not an object: the package has no ACL
				return cut(path, 2)
			}
			if len(path) == 4 {
				return cut(path, 4)
			}
			// packages/i/acl/env/service/version/method/apis/route/...
			if len(path) >= 9 && path[7] == "apis" {
				perm, _ := r.Get(with(cut(path, 6), "apisPermission")...)
				if perm == "restricted" {
					return cut(path, 9)
				}
			}
			return cut(path, 5)
		}
		return cut(path, 2)
	}
	return cut(path, 1)
}

// ---------------------------------------------------------------------------------------------
// oauth_token

// OAuthTokens: a token is read whole or refused; fields the spec does not list are ignored,
// except in user.restrictedTo, where an unknown restriction refuses the token rather than being
// ignored.
var OAuthTokens Policy = policyFuncs{closed: func(path []string) bool {
	return HasPrefix(path, p("user", "restrictedTo"))
}}
