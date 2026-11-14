package urac

import "github.com/soajs/soajs.controller.go/pkg/types"

// ACL is the user's ACL, merged from the packages its groups allow: acl for the gateway's
// environment and acl_all_env by environment (mw/mt/lib.js mergeACLArray).
type ACL struct {
	ACL       map[string]interface{}
	ACLAllEnv map[string]interface{}
}

// MergeACL merges package ACLs as Node.js mergeACLArray does. One package is taken as is. For
// several, service versions are merged in package order: a restricted version only merges into a
// restricted one and replaces any other, a version that is not restricted never changes a
// restricted one.
func MergeACL(packages []*types.Package) *ACL {
	if len(packages) == 1 {
		if packages[0] == nil {
			return nil
		}
		return &ACL{ACL: packages[0].ACL, ACLAllEnv: packages[0].ACLAllEnv}
	}
	out := &ACL{}
	first := true
	for _, p := range packages {
		if first || out.ACL == nil {
			out.ACL = deepCopyMap(p.ACL)
			first = false
			continue
		}
		mergeServices(out.ACL, p.ACL)
	}
	first = true
	for _, p := range packages {
		if first || out.ACLAllEnv == nil {
			out.ACLAllEnv = deepCopyMap(p.ACLAllEnv)
			first = false
			continue
		}
		for env, services := range p.ACLAllEnv {
			current, ok := out.ACLAllEnv[env].(map[string]interface{})
			if !ok {
				out.ACLAllEnv[env] = deepCopy(services)
				continue
			}
			s, _ := services.(map[string]interface{})
			mergeServices(current, s)
		}
	}
	return out
}

func mergeServices(acl, add map[string]interface{}) {
	for name, service := range add {
		current, ok := acl[name].(map[string]interface{})
		if !ok {
			acl[name] = deepCopy(service)
			continue
		}
		versions, _ := service.(map[string]interface{})
		for v, version := range versions {
			cur, ok := current[v].(map[string]interface{})
			if !ok {
				current[v] = deepCopy(version)
				continue
			}
			ver, _ := version.(map[string]interface{})
			curRestricted := cur["apisPermission"] == "restricted"
			if ver["apisPermission"] == "restricted" {
				if curRestricted {
					current[v] = lodashMerge(cur, deepCopy(ver))
				} else {
					current[v] = deepCopy(ver)
				}
				continue
			}
			if !curRestricted {
				current[v] = lodashMerge(cur, deepCopy(ver))
			}
		}
	}
}

// lodashMerge is _.merge for JSON values: objects merge key by key and arrays index by index,
// recursively; any other source value replaces the destination.
func lodashMerge(dst, src interface{}) interface{} {
	switch s := src.(type) {
	case map[string]interface{}:
		d, ok := dst.(map[string]interface{})
		if !ok {
			d = map[string]interface{}{}
		}
		for k, v := range s {
			d[k] = lodashMerge(d[k], v)
		}
		return d
	case []interface{}:
		d, ok := dst.([]interface{})
		if !ok {
			d = []interface{}{}
		}
		for i, v := range s {
			if i < len(d) {
				d[i] = lodashMerge(d[i], v)
			} else {
				d = append(d, lodashMerge(nil, v))
			}
		}
		return d
	}
	// null, as any other value, replaces the destination; only undefined is skipped by _.merge,
	// and a key absent from the source is never visited
	return src
}

func deepCopy(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return deepCopyMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = deepCopy(e)
		}
		return out
	}
	return v
}

func deepCopyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = deepCopy(v)
	}
	return out
}
