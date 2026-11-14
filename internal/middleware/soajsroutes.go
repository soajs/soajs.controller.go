package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/security"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// GatewayService is the gateway's own service name and version (Node.js param.init serviceName
// and serviceVersion).
type GatewayService struct {
	Name, Version string
}

// mimicGatewayAPI makes the request a call to an API of the gateway itself (Node.js
// mimicGatewayApi), so mt runs the key, OAuth and user checks for it.
func mimicGatewayAPI(params *types.ServiceParams, gw GatewayService, path string) {
	info := params.URLInfo
	info.ServiceName, info.ServiceVersion = gw.Name, gw.Version
	info.ServicePath, info.URL = path, path
	params.GatewayAPI = true
}

// soajsACL answers /soajs/acl (mw/soajsRoutes/keyACL, lib/acl.js computeACL) with what the
// caller may use: the user's ACL and packages, else the package's ACL and the key's package;
// finalACL, per service, the latest version and, for a restricted one, its APIs per method; the
// user's _id and the key's tenant.
func soajsACL(w http.ResponseWriter, soajsCtx *SOAJSContext) {
	params := soajsCtx.Controller.ServiceParams
	key := soajsCtx.Key
	if key == nil {
		WriteCode(w, 132)
		return
	}
	var acl map[string]interface{}
	var packages []interface{}
	var urac interface{}
	if user := soajsCtx.Urac; user != nil {
		acl = user.ACL()
		packages = user.AllowedPackages()
		if profile := user.Profile(false); profile != nil {
			urac = map[string]interface{}{"_id": profile["_id"]}
		}
	}
	if packages == nil && key.Application.Package != "" {
		packages = []interface{}{key.Application.Package}
	}
	if acl == nil && params.Package != nil && params.Package.ACL != nil {
		acl = deepCopyJSON(params.Package.ACL)
	}
	var tenant interface{}
	if key.Application.Package != "" {
		tenant = map[string]interface{}{"id": key.Tenant.ID, "name": key.Tenant.Name, "code": key.Tenant.Code, "type": key.Tenant.Type}
	}
	body, err := json.Marshal(map[string]interface{}{
		"acl":      acl,
		"finalACL": finalACLOf(acl, soajsCtx.Log),
		"packages": packages,
		"urac":     urac,
		"tenant":   tenant,
	})
	if err != nil {
		soajsCtx.Log.Error("soajs acl", zap.Error(err))
		WriteCode(w, 135)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// finalACLOf is, per service of an ACL, its latest version and, when that version is restricted,
// the APIs listed under each method, in a {apis} entry or in grouped [{group, apis}] entries. The
// APIs of a method are sorted. A version that is not an object is skipped.
func finalACLOf(acl map[string]interface{}, log *zap.Logger) map[string]interface{} {
	if acl == nil {
		return nil
	}
	final := map[string]interface{}{}
	for service, s := range acl {
		versions, _ := s.(map[string]interface{})
		latest := ""
		for v, version := range versions {
			if _, ok := version.(map[string]interface{}); !ok {
				log.Error("ACL: a version that is not an object is skipped", zap.String("service", service), zap.String("version", v))
				continue
			}
			if latest == "" || lib.IsLatest(v, latest) {
				latest = v
			}
		}
		if latest == "" {
			continue
		}
		entry := map[string]interface{}{"version": latest}
		version := versions[latest].(map[string]interface{})
		if version["apisPermission"] == "restricted" {
			for method, m := range version {
				if method == "access" || method == "apisPermission" {
					continue
				}
				groups, ok := m.([]interface{})
				if !ok {
					groups = []interface{}{m}
				}
				seen := map[string]bool{}
				var apis []string
				for _, g := range groups {
					group, _ := g.(map[string]interface{})
					list, _ := group["apis"].(map[string]interface{})
					for api := range list {
						if !seen[api] {
							seen[api] = true
							apis = append(apis, api)
						}
					}
				}
				if len(apis) > 0 {
					sort.Strings(apis)
					entry[method] = apis
				}
			}
		}
		final[service] = entry
	}
	return final
}

// proxyRoute prepares /soajs/proxy (lib/extractBuildParameters.js): the service is the first
// segment of the decoded proxyRoute when the registry has it, at its registry version, else 1; a
// key that is sent must decrypt (else 132). It answers false when it wrote the answer.
func proxyRoute(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext, reg registry.Registry) bool {
	params := soajsCtx.Controller.ServiceParams
	params.Proxy = true
	query := parseQueryString(r.URL.RawQuery)
	route, _ := decodeURIComponent(query.get("proxyRoute"))
	info := params.URLInfo
	info.ServiceName, info.ServiceVersion, info.ServicePath, info.URL = "", "1", "", route
	regData := reg.Get()
	if parts := strings.Split(route, "/"); len(parts) > 1 && regData != nil {
		if svc := regData.Services[parts[1]]; svc != nil {
			info.ServiceName = parts[1]
			if svc.Version != "" {
				info.ServiceVersion = svc.Version
			}
		}
	}
	if key := r.Header.Get("key"); key != "" {
		if regData == nil || regData.ServiceConfig == nil {
			WriteCode(w, 132)
			return false
		}
		keyConfig := security.KeyConfig{Algorithm: regData.ServiceConfig.Key.Algorithm, Password: regData.ServiceConfig.Key.Password}
		if _, err := security.DecryptKey(key, keyConfig); err != nil {
			soajsCtx.Log.Warn(err.Error())
			WriteCode(w, 132)
			return false
		}
	}
	return true
}

// soajsProxy forwards /soajs/proxy (mw/gotoService/proxyRequest.js) to its proxyRoute (else 139):
//   - the remote environment is the __env query parameter, else header, uppercased;
//   - the remote key is the extKey query parameter; else, with an environment, the last extKey
//     for it of the tenant tCode (query parameter, else the key's tenant): no tenant with the code
//     answers 139, no extKey 137;
//   - without an environment the route is the URL, with a 30 s timeout; with one, the URL is the
//     remote registry's protocol://[apiPrefix.]domain:port and the route, with the gateway's
//     requestTimeout: a registry that does not load answers 207, no gateway entry 131, a remote
//     registry without protocol, domain and port 208;
//   - the query is the request's without proxyRoute and __env, with the access_token header
//     added when the query has none; the headers are the request's with soajs_roaming, the
//     remote key or none, and no host;
//   - a URL that is not http or https, or a failed call, answers 135.
func soajsProxy(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext, reg registry.Registry, prov provision.Provision) {
	query := parseQueryString(r.URL.RawQuery)
	remoteENV := r.Header.Get("__env")
	if v := query.get("__env"); v != "" {
		remoteENV = v
	}
	remoteENV = strings.ToUpper(remoteENV)
	route, err := decodeURIComponent(query.get("proxyRoute"))
	if err != nil || route == "" {
		WriteCode(w, 139)
		return
	}
	tCode := query.get("tCode")
	if tCode == "" && soajsCtx.Tenant != nil {
		tCode = soajsCtx.Tenant.Code
	}
	remoteKey := query.get("extKey")
	if remoteKey == "" && tCode != "" && remoteENV != "" {
		extKey, found, err := prov.GetTenantEnvExtKey(tCode, remoteENV)
		if err != nil || !found {
			soajsCtx.Log.Error("proxy: no tenant for the code", zap.String("tCode", tCode), zap.Error(err))
			WriteCode(w, 139)
			return
		}
		if extKey == "" {
			soajsCtx.Log.Error("No remote key found for tenant: " + tCode + " in environment: " + remoteENV)
			WriteCode(w, 137)
			return
		}
		remoteKey = extKey
	}

	uri, timeout := route, 30*time.Second
	if remoteENV != "" {
		remote, err := reg.LoadByEnv(remoteENV, false)
		if err != nil || remote == nil {
			soajsCtx.Log.Error("proxy: unable to load the registry", zap.String("env", remoteENV), zap.Error(err))
			WriteCode(w, 207)
			return
		}
		gw := reg.Get().Services["controller"]
		if gw == nil {
			WriteCode(w, 131)
			return
		}
		if remote.Protocol == "" || remote.Domain == "" || remote.Port == 0 {
			WriteCode(w, 208)
			return
		}
		prefix := ""
		if remote.APIPrefix != "" {
			prefix = remote.APIPrefix + "."
		}
		uri = remote.Protocol + "://" + prefix + remote.Domain + ":" + strconv.Itoa(remote.Port) + route
		timeout = time.Duration(gw.RequestTimeout) * time.Second
	}
	target, err := lib.ParseAbsoluteURL(uri)
	if err != nil {
		soajsCtx.Log.Error("proxy: " + err.Error())
		WriteCode(w, 135)
		return
	}
	query.remove("proxyRoute")
	query.remove("__env")
	if query.get("access_token") == "" {
		if token := r.Header.Get("access_token"); token != "" {
			query.set("access_token", token)
		}
	}
	host := target.Host
	if target.Port != "" {
		host = net.JoinHostPort(strings.Trim(target.Host, "[]"), target.Port)
	}
	address := target.Scheme + "://" + host + target.Pathname
	if qs := query.encode(); qs != "" {
		address += "?" + qs
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	idle := newIdleTimer(timeout, cancel)
	defer idle.stop()
	var body io.Reader = http.NoBody
	if r.Body != nil && r.Body != http.NoBody {
		body = idleReader{r.Body, idle}
	}
	out, err := http.NewRequestWithContext(ctx, r.Method, address, body)
	if err != nil {
		soajsCtx.Log.Error("proxy: " + err.Error())
		WriteCode(w, 135)
		return
	}
	out.ContentLength = r.ContentLength
	out.Header = r.Header.Clone()
	out.Header["soajs_roaming"] = []string{strings.ToLower(reg.GetEnvironment())}
	out.Header.Del("key")
	if remoteKey != "" {
		out.Header.Set("key", remoteKey)
	}
	resp, err := proxyTransport.RoundTrip(out)
	if err != nil {
		soajsCtx.Log.Error("proxy: " + err.Error())
		WriteCode(w, 135)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		w.Header()[k] = append([]string(nil), vv...)
	}
	w.WriteHeader(resp.StatusCode)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := io.Copy(w, idleReader{resp.Body, idle}); err != nil {
		soajsCtx.Log.Error("proxy: " + err.Error())
	}
}

// idleTimer cancels a call after a time without traffic (the Node.js request timeout option).
type idleTimer struct {
	mu      sync.Mutex
	t       *time.Timer
	timeout time.Duration
}

func newIdleTimer(timeout time.Duration, cancel context.CancelFunc) *idleTimer {
	if timeout <= 0 {
		return &idleTimer{}
	}
	return &idleTimer{t: time.AfterFunc(timeout, cancel), timeout: timeout}
}

func (i *idleTimer) touch() {
	if i.t == nil {
		return
	}
	i.mu.Lock()
	i.t.Reset(i.timeout)
	i.mu.Unlock()
}

func (i *idleTimer) stop() {
	if i.t != nil {
		i.t.Stop()
	}
}

type idleReader struct {
	r    io.Reader
	idle *idleTimer
}

func (i idleReader) Read(p []byte) (int, error) {
	n, err := i.r.Read(p)
	if n > 0 {
		i.idle.touch()
	}
	return n, err
}

// queryString is a query string as Node.js gives it to the gateway (Object.fromEntries of the
// URL's searchParams): a repeated parameter keeps the place of its first occurrence and its last
// value.
type queryString struct {
	keys   []string
	values map[string]string
}

// parseQueryString parses a query string as URLSearchParams does: "+" is a space, a %xx that
// does not decode is kept as is.
func parseQueryString(raw string) *queryString {
	q := &queryString{values: map[string]string{}}
	for _, pair := range strings.Split(raw, "&") {
		if pair == "" {
			continue
		}
		k, v, _ := strings.Cut(pair, "=")
		q.set(unescapeQuery(k), unescapeQuery(v))
	}
	return q
}

// get is the value of a parameter, "" without one.
func (q *queryString) get(k string) string {
	return q.values[k]
}

func (q *queryString) remove(k string) {
	if _, ok := q.values[k]; !ok {
		return
	}
	delete(q.values, k)
	for i, e := range q.keys {
		if e == k {
			q.keys = append(q.keys[:i], q.keys[i+1:]...)
			break
		}
	}
}

func (q *queryString) set(k, v string) {
	if _, ok := q.values[k]; !ok {
		q.keys = append(q.keys, k)
	}
	q.values[k] = v
}

// encode is querystring.stringify: each parameter as k=v, escaped as encodeURIComponent does.
func (q *queryString) encode() string {
	parts := make([]string, 0, len(q.keys))
	for _, k := range q.keys {
		parts = append(parts, escapeComponent(k)+"="+escapeComponent(q.values[k]))
	}
	return strings.Join(parts, "&")
}

func unescapeQuery(s string) string {
	s = strings.ReplaceAll(s, "+", " ")
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) && isHexDigit(s[i+1]) && isHexDigit(s[i+2]) {
			n, _ := strconv.ParseUint(s[i+1:i+3], 16, 8)
			b.WriteByte(byte(n))
			i += 2
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// decodeURIComponent decodes every %xx; a % that does not start one is an error.
func decodeURIComponent(s string) (string, error) {
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && (i+2 >= len(s) || !isHexDigit(s[i+1]) || !isHexDigit(s[i+2])) {
			return "", url.EscapeError(s[i:])
		}
	}
	return url.PathUnescape(s)
}

func isHexDigit(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// escapeComponent is encodeURIComponent: every byte but letters, digits and -_.!~*'() is %XX.
func escapeComponent(s string) string {
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
