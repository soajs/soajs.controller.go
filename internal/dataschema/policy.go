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
	r.ReadBool(p("locked"), "soajs.core.modules soajs.core/provision/mongo.js:310 (!!locked)")
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
	apps, _ := r.Get("applications")
	if list, isList := apps.([]interface{}); isList {
		for i := range list {
			readTTL(r, p("applications", strconv.Itoa(i), "_TTL"))
		}
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
			if len(path) == 3 {
				// keys missing or not a list: the application
				return cut(path, 2)
			}
			if len(path) == 4 || path[4] != "extKeys" {
				return cut(path, 4)
			}
			if len(path) == 5 {
				// extKeys missing or not a list: the key
				return cut(path, 4)
			}
			if len(path) == 6 {
				return cut(path, 6)
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

// UnreadableTTL is the cache time, in ms, of a package or key whose _TTL is not a number, and of a
// package the gateway could not read: Node.js reloads such a package on every use (a NaN _TTL is
// never fresh); Go reloads it after a minute, so a fix applies without /loadProvision.
const UnreadableTTL = 60000

// readTTL reads a _TTL as Node.js uses it in arithmetic: a numeric string is its number, anything
// else reloads soon (soajs.core.modules soajs.provision/index.js:201-205).
func readTTL(r *Reading, path []string) {
	v, ok := r.Get(path...)
	if !ok {
		return
	}
	switch t := v.(type) {
	case float64:
		if t == float64(int64(t)) {
			return
		}
		r.Set(path, float64(int64(t)))
	case string:
		if t == "" {
			r.Set(path, 0.0)
			break
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			r.Set(path, float64(int64(f)))
			break
		}
		r.Set(path, float64(UnreadableTTL))
	default:
		if !Truthy(v) {
			r.Set(path, 0.0)
			break
		}
		r.Set(path, float64(UnreadableTTL))
	}
	r.ReadAs(path, "soajs.core.modules soajs.provision/index.js:201-205 (_TTL in arithmetic: a numeric string is its number, NaN is never fresh)")
}

func prepareProduct(r *Reading) {
	pkgs, _ := r.Get("packages")
	list, isList := pkgs.([]interface{})
	if !isList {
		return
	}
	for i := range list {
		pi := strconv.Itoa(i)
		readTTL(r, p("packages", pi, "_TTL"))
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

// ---------------------------------------------------------------------------------------------
// marketplace

var versionFlags = []string{"extKeyRequired", "urac", "urac_Profile", "urac_ACL", "urac_Config", "urac_GroupConfig",
	"tenant_Profile", "provision_ACL", "oauth"}

// Marketplace: an unreadable version is left out, unless it is the service's latest: then the
// service is skipped (130), so calls without a version never go to an older version. The service
// is skipped too when its name, type, port or versions list cannot be read.
var Marketplace Policy = policyFuncs{prepare: prepareMarket, unit: marketUnit}

func prepareMarket(r *Reading) {
	const ref = "soajs.controller modules/registry/index.js:187-213 (value || default)"
	if cfg, ok := r.Get("configuration"); ok {
		if m, isMap := cfg.(map[string]interface{}); isMap {
			for field, def := range map[string]interface{}{"group": "service", "requestTimeoutRenewal": 0.0, "requestTimeout": 30.0} {
				if v, has := m[field]; has && !Truthy(v) {
					m[field] = def
					r.ReadAs(p("configuration", field), ref)
				}
			}
		}
	}
	vs, _ := r.Get("versions")
	list, _ := vs.([]interface{})
	for i := range list {
		ver, isMap := list[i].(map[string]interface{})
		if !isMap {
			continue
		}
		vi := strconv.Itoa(i)
		if r.Collection == "marketplace" && ver["version"] != nil {
			for _, flag := range versionFlags {
				v, has := ver[flag]
				if !has {
					continue
				}
				if _, isBool := v.(bool); !isBool {
					if Truthy(v) {
						// value || false: a truthy value is kept as it is in the registry
						r.Raw[MarketRawKey(ver["version"], flag)] = v
					}
					ver[flag] = Truthy(v)
					r.ReadAs(p("versions", vi, flag), ref)
				}
			}
		}
		for _, field := range []string{"interConnect", "maintenance"} {
			if v, has := ver[field]; has && !Truthy(v) {
				delete(ver, field)
				r.ReadAs(p("versions", vi, field), "soajs.controller modules/registry/index.js:212-213, mw/mt/index.js:318 (a falsy "+field+" is none)")
			}
		}
	}
}

// MarketRawKey is the Result.Raw key of a version's flag: by version number, as versions may be
// left out after the reading.
func MarketRawKey(version interface{}, flag string) string {
	v, _ := version.(string)
	return "version:" + v + "/" + flag
}

func marketUnit(r *Reading, path []string) []string {
	if len(path) == 0 {
		return nil
	}
	switch path[0] {
	case "configuration":
		if len(path) >= 2 && (path[1] == "group" || path[1] == "requestTimeout" || path[1] == "requestTimeoutRenewal" || path[1] == "subType") {
			return cut(path, 2)
		}
		return nil
	case "versions":
		if len(path) == 1 {
			return nil
		}
		if isLatestVersion(r, path[1]) {
			return nil
		}
		return cut(path, 2)
	case "_id", "name", "type":
		return nil
	}
	return cut(path, 1)
}

// isLatestVersion reports whether the version at index i is the latest of the document, or may
// be: a version whose own number cannot be read may be the latest.
func isLatestVersion(r *Reading, i string) bool {
	vs, _ := r.Get("versions")
	list, _ := vs.([]interface{})
	idx, err := strconv.Atoi(i)
	if err != nil || idx < 0 || idx >= len(list) {
		return true
	}
	num := func(v interface{}) (int, bool) {
		m, _ := v.(map[string]interface{})
		s, isString := m["version"].(string)
		if !isString || s == "" {
			return 0, false
		}
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	mine, ok := num(list[idx])
	if !ok {
		return true
	}
	for j, v := range list {
		if j == idx {
			continue
		}
		if n, ok := num(v); ok && n > mine {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------------------------
// custom_registry

// CustomRegistry: in the gateway entry each feature (traffic, events, mcp, …) is left out on its
// own, and inside the features that restrict traffic only the broken part: a throttling strategy,
// an ip2ban address, a maintenance mode field other than on. In the urac and oauth entries each
// setting is left out on its own, and an access resolver or a roaming entry alone. The entry is
// skipped when its _id, name, created or value cannot be read, or the urac masterDB.
var CustomRegistry Policy = policyFuncs{prepare: prepareCustom, unit: customUnit}

func prepareCustom(r *Reading) {
	if envs, ok := r.Get("sharedEnvs"); ok {
		if m, isMap := envs.(map[string]interface{}); isMap {
			for _, env := range keysOf(m) {
				r.NoteBool(p("sharedEnvs", env), "soajs.controller modules/registry/mongo.js:40, 52 (sharedEnvs[env], truthiness)")
			}
		}
	}
	if name, _ := r.Get("name"); name != "gateway" {
		return
	}
	if mm, ok := r.Get("value", "maintenanceMode"); ok {
		const ref = "soajs.controller mw/maintenanceMode/index.js:18-27 (on, truthiness; status || 503; message || default; retryAfter if truthy)"
		m, isMap := mm.(map[string]interface{})
		if !isMap {
			r.Drop(p("value", "maintenanceMode"), ref, ReadAsNode)
		} else {
			r.NoteBool(p("value", "maintenanceMode", "on"), ref)
			for _, f := range []string{"status", "message", "retryAfter"} {
				if v, has := m[f]; has && !Truthy(v) {
					delete(m, f)
					r.ReadAs(p("value", "maintenanceMode", f), ref)
				}
			}
		}
	}
	if ban, ok := r.Get("value", "traffic", "ip2ban"); ok {
		const ref = "soajs.controller mw/ip2ban/index.js:43 (Array.isArray(ip2ban) && ip2ban.includes(ip))"
		list, isList := ban.([]interface{})
		if !isList {
			r.Drop(p("value", "traffic", "ip2ban"), ref, ReadAsNode)
		} else {
			for _, i := range indexesDesc(list) {
				if _, isString := list[i].(string); !isString {
					r.Drop(p("value", "traffic", "ip2ban", strconv.Itoa(i)), ref, ReadAsNode)
				}
			}
		}
	}
	if wl, ok := r.Get("value", "mt", "whitelist"); ok {
		const ref = "soajs.controller mw/mt/utils.js:269-291 (ips only as an array; acl and oauth, truthiness)"
		if m, isMap := wl.(map[string]interface{}); isMap {
			if _, isList := m["ips"].([]interface{}); !isList {
				r.Drop(p("value", "mt", "whitelist"), ref, ReadAsNode)
			} else {
				r.NoteBool(p("value", "mt", "whitelist", "acl"), ref)
				r.NoteBool(p("value", "mt", "whitelist", "oauth"), ref)
			}
		}
	}
	// on unless false (mw/oauth/index.js:60): the gateway checks it the same way
	r.NoteBool(p("value", "oauth", "deviceIdCheck"), "soajs.controller mw/oauth/index.js:54-60 (on unless === false)")
}

func customUnit(r *Reading, path []string) []string {
	if len(path) == 0 {
		return nil
	}
	switch path[0] {
	case "_id", "name", "created":
		return nil
	case "sharedEnvs":
		if len(path) >= 2 {
			// one environment's entry, never the others
			return cut(path, 2)
		}
		return cut(path, 1)
	case "value":
		if len(path) == 1 {
			return nil
		}
		name, _ := r.Get("name")
		switch name {
		case "gateway":
			if path[1] == "traffic" {
				// traffic restricts: one leaf at a time, ip2ban by address (prepareCustom)
				if len(path) >= 4 && path[2] == "ip2ban" {
					return cut(path, 4)
				}
				if len(path) >= 3 {
					return cut(path, 3)
				}
				return cut(path, 2)
			}
			if len(path) >= 3 && path[1] == "maintenanceMode" && path[2] != "on" {
				return cut(path, 3)
			}
			return cut(path, 2)
		case "urac":
			if path[1] == "masterDB" {
				return nil
			}
			if len(path) >= 3 && (path[1] == "tenantAccessResolvers" || path[1] == "autoRoaming") {
				return cut(path, 3)
			}
			return cut(path, 2)
		case "oauth":
			return cut(path, 2)
		}
		return nil
	}
	return cut(path, 1)
}

// ---------------------------------------------------------------------------------------------
// environment

// envBooleans are the environment's flags Node.js reads with JavaScript truthiness.
var envBooleans = []struct {
	path []string
	ref  string
}{
	{p("services", "controller", "authorization"), "soajs.controller mw/gotoService/redirectToService.js:306"},
	{p("services", "config", "awareness", "autoRegisterService"), "soajs.controller modules/registry/index.js:428"},
	{p("services", "config", "cors", "enabled"), "soajs.controller mw/cors/index.js:17"},
	{p("services", "config", "cors", "credentials"), "soajs.controller mw/cors/index.js:20 (credentials || 'true')"},
	{p("services", "config", "oauth", "getUserFromToken"), "soajs.controller mw/mt/urac.js:41"},
}

// envDroppable are the environment's fields that can be left out on their own: none of them
// decides access. Any other field that cannot be read leaves the environment unread, so the
// gateway never falls back to a default wider than the data (an origin of "*", the default key
// password, open CORS methods): the registry is not built and the previous one stays.
var envDroppable = [][]string{
	p("description"), p("locked"), p("settings"),
	p("services", "config", "logger"),
	p("services", "config", "awareness"),
	p("services", "controller", "requestTimeout"),
	p("services", "controller", "requestTimeoutRenewal"),
	p("services", "controller", "maxPoolSize"),
}

// CORSOriginRefused is the rule of a CORS origin the gateway does not send: Node.js sends the
// value as it is, which browsers refuse.
const CORSOriginRefused = "soajs.controller mw/cors/index.js:19, 26 (a origin that is not a string is sent as is, which browsers refuse; Go sends no Access-Control-Allow-Origin)"

// Environment: a field that does not decide access is left out on its own (envDroppable); the
// environment cannot be read when any other field cannot be.
var Environment Policy = policyFuncs{prepare: prepareEnvironment, unit: func(r *Reading, path []string) []string {
	for _, d := range envDroppable {
		if HasPrefix(path, d) {
			return cut(path, len(path))
		}
	}
	return nil
}}

func prepareEnvironment(r *Reading) {
	for _, b := range envBooleans {
		r.ReadBool(b.path, b.ref+" (truthiness)")
	}
	corsPath := p("services", "config", "cors")
	if origin, ok := r.Get(with(corsPath, "origin")...); ok {
		if _, isString := origin.(string); !isString {
			r.Raw[Pointer(with(corsPath, "origin"))] = origin
			list, isList := origin.([]interface{})
			one, _ := firstString(list)
			switch {
			case !Truthy(origin):
				r.Drop(with(corsPath, "origin"), "soajs.controller mw/cors/index.js:19 (origin || '*')", ReadAsNode)
			case isList && len(list) == 1 && one != "":
				r.Set(with(corsPath, "origin"), one)
				r.ReadAs(with(corsPath, "origin"), "soajs.controller mw/cors/index.js:26 (a list of one origin is that header)")
			default:
				r.Drop(with(corsPath, "origin"), CORSOriginRefused, ReadAsNode)
			}
		}
	}
	for _, field := range []string{"methods", "headers"} {
		v, _ := r.Get(with(corsPath, field)...)
		if list, isList := v.([]interface{}); isList {
			if joined, ok := joinStrings(list); ok {
				r.Set(with(corsPath, field), joined)
				r.ReadAs(with(corsPath, field), "soajs.controller mw/cors/index.js:29-37 (a list is sent as one header line per item)")
			}
		}
	}
	readNumber(r, with(corsPath, "maxage"), "soajs.controller mw/cors/index.js:23, 30 (maxage, a number in the header)")
	if th, ok := r.Get("services", "config", "throttling"); ok {
		if m, isMap := th.(map[string]interface{}); isMap {
			for _, name := range keysOf(m) {
				if _, isStrategy := m[name].(map[string]interface{}); !isStrategy {
					continue
				}
				for _, f := range []string{"type", "window", "limit", "retries", "delay"} {
					readNumber(r, p("services", "config", "throttling", name, f), "soajs.controller mw/traffic (a numeric string is used as a number)")
				}
			}
		}
	}
	dbs, _ := r.Get("dbs", "databases")
	if m, isMap := dbs.(map[string]interface{}); isMap {
		for _, name := range keysOf(m) {
			r.ReadBool(p("dbs", "databases", name, "tenantSpecific"), "soajs.controller modules/registry/index.js:86 (truthiness)")
		}
	}
}

// readNumber reads a numeric string as the number Node.js uses in arithmetic.
func readNumber(r *Reading, path []string, ref string) {
	v, ok := r.Get(path...)
	if !ok {
		return
	}
	s, isString := v.(string)
	if !isString {
		return
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return
	}
	r.Raw[Pointer(path)] = v
	r.Set(path, f)
	r.ReadAs(path, ref)
}

func firstString(list []interface{}) (string, bool) {
	if len(list) == 0 {
		return "", false
	}
	s, ok := list[0].(string)
	return s, ok
}

func joinStrings(list []interface{}) (string, bool) {
	out := ""
	for i, e := range list {
		s, ok := e.(string)
		if !ok {
			return "", false
		}
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out, len(list) > 0
}

// ---------------------------------------------------------------------------------------------
// users and groups

// Users: an invitation (config.allowedTenants[] item), a membership, or groups that cannot be read
// are left out on their own; without groups, as Node.js, the key's package ACL applies. The user
// cannot be read when its _id, username, status, tenant or a config.keys entry cannot be: a
// config.keys entry overrides the key's configuration.
var Users Policy = policyFuncs{prepare: prepareUser, unit: func(r *Reading, path []string) []string {
	if len(path) == 0 {
		return nil
	}
	switch path[0] {
	case "config":
		// a config.keys entry overrides the key's configuration, gateway settings included: it is
		// never left out, the user is not read
		if len(path) >= 3 && path[1] == "allowedTenants" {
			return cut(path, 3)
		}
		if len(path) == 2 && path[1] == "allowedTenants" {
			return cut(path, 2)
		}
		return nil
	case "memberships":
		if len(path) >= 2 {
			return cut(path, 2)
		}
		return cut(path, 1)
	case "groups":
		// no roles: as Node.js, the key's package ACL applies (reported as falling back)
		return cut(path, 1)
	case "email", "phone", "firstName", "lastName", "profile":
		return cut(path, 1)
	}
	return nil
}}

func prepareUser(r *Reading) {
	if v, ok := r.Get("config", "allowedTenants"); ok {
		if _, isList := v.([]interface{}); !isList {
			r.Drop(p("config", "allowedTenants"), "soajs.controller mw/mt/urac.js:81-85 (Array.isArray(allowedTenants))", ReadAsNode)
		}
	}
}

// Groups: a product's allowedPackages that cannot be read is left out, and a group that cannot be
// read gives no packages: as Node.js, the key's package ACL applies when nothing else gives one.
// allowedEnvironments, which the gateway does not use, is left out alone.
var Groups Policy = policyFuncs{prepare: prepareGroup, unit: func(r *Reading, path []string) []string {
	if len(path) >= 2 && path[0] == "config" && path[1] == "allowedEnvironments" {
		return cut(path, 2)
	}
	if len(path) >= 3 && path[0] == "config" && path[1] == "allowedPackages" {
		return cut(path, 3)
	}
	return nil
}}

// ---------------------------------------------------------------------------------------------

// PolicyFor is the policy that reads a collection: the gateway, /schemaProblems and schemacheck
// read every document the same way.
func PolicyFor(coll string) Policy {
	switch coll {
	case "tenants":
		return Tenants
	case "products":
		return Products
	case "marketplace":
		return Marketplace
	case "custom_registry":
		return CustomRegistry
	case "environment":
		return Environment
	case "users":
		return Users
	case "groups":
		return Groups
	case "oauth_token":
		return OAuthTokens
	}
	return WholeDocument
}

// prepareGroup reads allowedPackages as the urac driver merges it (lib/user.js:299-316): a list or a
// string is kept as it is (a string alone is not a list of packages, so the key's package ACL
// applies; next to another group's list, its characters are merged in); any other value cannot
// be merged and is left out. Both fall back to the key's package.
func prepareGroup(r *Reading) {
	const ref = "soajs.urac.driver lib/user.js:299-316 (allowedPackages merged as [...new Set([...a, ...b])]); soajs.controller mw/mt/urac.js:153-180"
	v, ok := r.Get("config", "allowedPackages")
	if !ok {
		return
	}
	m, isMap := v.(map[string]interface{})
	if !isMap {
		r.Drop(p("config", "allowedPackages"), ref, FellBackToKeyPackage)
		return
	}
	for _, product := range keysOf(m) {
		switch m[product].(type) {
		case []interface{}:
		case string:
			r.add(p("config", "allowedPackages", product), ref, FellBackToKeyPackage, nil)
		default:
			r.Drop(p("config", "allowedPackages", product), ref, FellBackToKeyPackage)
		}
	}
}
