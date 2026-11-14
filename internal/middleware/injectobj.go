package middleware

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/internal/urac"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// defaultMaxInjectSize is the soajsinjectobj size limit without SOAJS_MAX_INJECT_HEADER_SIZE.
const defaultMaxInjectSize = 65536

// field is one key of a JSON object written in order.
type field struct {
	key   string
	value interface{}
}

// object is a JSON object that keeps its key order, as a JavaScript object does.
type object []field

func (o object) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, f := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		k, _ := json.Marshal(f.key)
		b.Write(k)
		b.WriteByte(':')
		v, err := marshalNoEscape(f.value)
		if err != nil {
			return nil, err
		}
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// set replaces the value of key, keeping its place, or appends it.
func (o *object) set(key string, value interface{}) {
	for i := range *o {
		if (*o)[i].key == key {
			(*o)[i].value = value
			return
		}
	}
	*o = append(*o, field{key, value})
}

func (o *object) has(key string) bool {
	for _, f := range *o {
		if f.key == key {
			return true
		}
	}
	return false
}

func (o *object) remove(key string) {
	for i, f := range *o {
		if f.key == key {
			*o = append((*o)[:i], (*o)[i+1:]...)
			return
		}
	}
}

// marshalNoEscape is JSON.stringify for these values: <, > and & are not escaped.
func marshalNoEscape(v interface{}) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

// encodeHeaderValue is lib/header.js encodeHeaderValue: encodeURIComponent of the value, "" for
// none.
func encodeHeaderValue(v interface{}) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		raw, _ := json.Marshal(v)
		s = string(raw)
	}
	return encodeURIComponent(s)
}

// encodeURIComponent escapes everything but A-Z a-z 0-9 - _ . ! ~ * ' ( ), as JavaScript does.
func encodeURIComponent(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// versionFlagsOf are the urac and provision flags of the request's service version.
func versionFlagsOf(reg *types.RegistryData, service, version string) *types.VersionInfo {
	if reg == nil || reg.Services[service] == nil || reg.Services[service].Versions[version] == nil {
		return &types.VersionInfo{}
	}
	return reg.Services[service].Versions[version]
}

// BuildInjectObj builds the soajsinjectobj header of a key's API, as the Node.js mt does
// (mw/mt/index.js:196-388): the tenant, the key with its configuration for the service, the
// application, the package's ACL, the device and geo, the user, the awareness of the gateway and
// of the service's interConnect peers. ok is false when the header is larger than
// SOAJS_MAX_INJECT_HEADER_SIZE (135).
func BuildInjectObj(soajsCtx *SOAJSContext, reg registry.Registry, aw awareness.Awareness, r *httpRequestInfo) (string, bool) {
	params := soajsCtx.Controller.ServiceParams
	key := soajsCtx.Key
	regData := reg.Get()
	serviceName := params.URLInfo.ServiceName
	flags := versionFlagsOf(regData, serviceName, params.URLInfo.ServiceVersion)

	// key.config: the commonFields flattened, then the service's entry, from the services
	// configuration (the key's, with the user's merged in)
	dataServiceConfig := soajsCtx.ServicesConfig
	if dataServiceConfig == nil {
		dataServiceConfig = key.Config
	}
	var serviceConfig object
	if common, ok := dataServiceConfig["commonFields"].(map[string]interface{}); ok {
		for _, k := range sortedMapKeys(common) {
			if !serviceConfig.has(k) || !truthy(lookup(serviceConfig, k)) {
				serviceConfig.set(k, common[k])
			}
		}
	}
	if v, ok := dataServiceConfig[serviceName]; ok && truthy(v) {
		serviceConfig.set(serviceName, v)
	}

	tenant := object{
		{"id", key.Tenant.ID}, {"name", key.Tenant.Name}, {"code", key.Tenant.Code}, {"locked", key.Tenant.Locked},
	}
	if soajsCtx.Roaming != nil {
		ro := soajsCtx.Roaming
		roamingObj := object{{"tId", ro.TID}, {"id", ro.ID}, {"user", ro.User}, {"code", ro.Code}}
		if ro.TenantMetaDB != nil {
			roamingObj = append(roamingObj, field{"tenantMetaDB", ro.TenantMetaDB})
		}
		tenant = append(tenant, field{"roaming", roamingObj})
	}
	tenant = append(tenant, field{"type", key.Tenant.Type})
	if key.Tenant.Main != nil {
		tenant = append(tenant, field{"main", key.Tenant.Main})
	}
	if flags.TenantProfile && truthy(key.Tenant.Profile) {
		tenant = append(tenant, field{"profile", key.Tenant.Profile})
	}

	application := object{{"product", key.Application.Product}, {"package", key.Application.Package}, {"appId", key.Application.AppID}}
	pkg := object{}
	if params.Package != nil {
		if params.Package.ACL != nil {
			pkg = append(pkg, field{"acl", params.Package.ACL})
		}
		if params.Package.ACLAllEnv != nil {
			pkg = append(pkg, field{"acl_all_env", params.Package.ACLAllEnv})
		}
	}

	inject := object{
		{"tenant", tenant},
		{"key", object{{"config", &serviceConfig}, {"iKey", key.InternalKey}, {"eKey", key.ExtKey}}},
		{"application", application},
		{"package", &pkg},
	}
	if !params.SkipACL {
		if r.userAgent != "" {
			inject = append(inject, field{"device", r.userAgent})
		}
		geo := object{}
		if r.clientIP != "" {
			geo = append(geo, field{"ip", r.clientIP})
		}
		inject = append(inject, field{"geo", geo})
	}

	if soajsCtx.Urac != nil && flags.Urac {
		if profile := soajsCtx.Urac.Profile(flags.UracConfig || flags.UracGroupConfig); profile != nil {
			userTenant, _ := profile["tenant"].(map[string]interface{})
			u := object{
				{"_id", profile["_id"]},
				{"username", profile["username"]},
				{"firstName", encodeHeaderValue(profile["firstName"])},
				{"lastName", encodeHeaderValue(profile["lastName"])},
			}
			if v, ok := profile["email"]; ok {
				u = append(u, field{"email", v})
			}
			if v, ok := profile["groups"]; ok {
				u = append(u, field{"groups", v})
			}
			u = append(u, field{"tenant", object{{"id", userTenant["id"]}, {"code", userTenant["code"]}}})
			membership := ""
			if gw, ok := regData.CustomRegistry["gateway"].(map[string]interface{}); ok {
				value, _ := gw["value"].(map[string]interface{})
				config, _ := value["membership"].(map[string]interface{})
				membership = urac.ResolveMembership(config, key.Application.Product, profile["memberships"])
			}
			if membership != "" {
				u = append(u, field{"membership", membership})
			}
			if flags.UracProfile {
				u = appendPresent(u, "profile", profile)
			}
			if flags.UracACL {
				u = append(u, field{"acl", nullable(soajsCtx.Urac.ACL())}, field{"acl_AllEnv", nullable(soajsCtx.Urac.ACLAllEnv())})
			}
			if flags.UracConfig {
				u = appendPresent(u, "config", profile)
			}
			if flags.UracGroupConfig {
				u = appendPresent(u, "groupsConfig", profile)
			}
			inject = append(inject, field{"urac", u}, field{"param", object{{"urac_Profile", flags.UracProfile}, {"urac_ACL", flags.UracACL}}})
		}
	}

	if !flags.ProvisionACL {
		pkg.remove("acl_all_env")
		pkg.remove("acl")
	}

	// awareness: the gateway's host in this environment; without it there is none, and no
	// interConnect (Node.js throws there)
	if aw != nil {
		if gw, err := aw.GetHost("controller", ""); err == nil {
			port := 0
			if regData.ServiceConfig != nil {
				port = regData.ServiceConfig.Ports.Controller
			}
			awarenessObj := object{{"host", gw.Host}, {"port", port}}
			if len(flags.InterConnect) > 0 {
				peers := []interface{}{}
				for _, item := range flags.InterConnect {
					if item.Name == "" || regData.Services[item.Name] == nil || regData.Services[item.Name].Port == 0 {
						continue
					}
					if v, ok := dataServiceConfig[item.Name]; ok && truthy(v) {
						serviceConfig.set(item.Name, v)
					}
					var version interface{}
					if item.Version != "" {
						version = item.Version
					}
					host, err := aw.GetHost(item.Name, item.Version)
					if err != nil {
						soajsCtx.Log.Error(serviceName + " interConnect failed for [" + item.Name + "@" + item.Version + "], peer dropped from awareness")
						continue
					}
					peer := object{{"name", item.Name}, {"version", version}, {"host", host.Host}, {"port", regData.Services[item.Name].Port}}
					if latest := aw.GetLatestVersion(item.Name); latest != "" {
						peer = append(peer, field{"latest", latest})
					}
					peers = append(peers, peer)
				}
				awarenessObj = append(awarenessObj, field{"interConnect", peers})
			}
			inject = append(inject, field{"awareness", awarenessObj})
		}
	}

	raw, err := marshalNoEscape(inject)
	if err != nil {
		return "", false
	}
	max := defaultMaxInjectSize
	if v, err := strconv.Atoi(os.Getenv("SOAJS_MAX_INJECT_HEADER_SIZE")); err == nil {
		max = v
	}
	if len(utf16.Encode([]rune(string(raw)))) > max {
		soajsCtx.Log.Error("soajsinjectobj header exceeds maximum size")
		return "", false
	}
	return string(raw), true
}

// httpRequestInfo is what the injectObj takes from the request.
type httpRequestInfo struct {
	userAgent string
	clientIP  string
}

func appendPresent(o object, key string, m map[string]interface{}) object {
	if v, ok := m[key]; ok {
		return append(o, field{key, v})
	}
	return o
}

func nullable(m map[string]interface{}) interface{} {
	if m == nil {
		return nil
	}
	return m
}

func lookup(o object, key string) interface{} {
	for _, f := range o {
		if f.key == key {
			return f.value
		}
	}
	return nil
}

func sortedMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
