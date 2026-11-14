package middleware

import (
	"encoding/json"
	"errors"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// mockAwarenessProxy implements awareness.Awareness for proxy tests
type mockAwarenessProxy struct {
	hosts  map[string]*types.HostInfo
	latest map[string]string
}

func (m *mockAwarenessProxy) GetHost(serviceName, version string) (*types.HostInfo, error) {
	key := serviceName + ":" + version
	if h, ok := m.hosts[key]; ok {
		return h, nil
	}
	return nil, awareness.ErrNoHost
}

func (m *mockAwarenessProxy) GetLatestVersion(name string) string { return m.latest[name] }

func (m *mockAwarenessProxy) GetLatestVersionFromCluster(string) (string, bool) { return "", false }

func (m *mockAwarenessProxy) Stats() map[string]map[string]map[string]*awareness.HostStat {
	return nil
}

// mockRegistryProxy implements registry.Registry for proxy tests
type mockRegistryProxy struct {
	data *types.RegistryData
	// others, when set, are the registries of the other environments, by lowercase code
	others map[string]*types.RegistryData
}

func (m *mockRegistryProxy) Load(env string) error                    { return nil }
func (m *mockRegistryProxy) Reload() error                            { return nil }
func (m *mockRegistryProxy) Get() *types.RegistryData                 { return m.data }
func (m *mockRegistryProxy) RegisterHost(info types.HostRecord) error { return nil }
func (m *mockRegistryProxy) GetHost(serviceName, version string) (*types.HostInfo, error) {
	return nil, nil
}
func (m *mockRegistryProxy) GetEnvironment() string { return "test" }
func (m *mockRegistryProxy) OtherEnvControllerHosts(string) ([]types.HostRecord, error) {
	return nil, nil
}
func (m *mockRegistryProxy) UpsertEnvController(string, string, int64, map[string]interface{}) error {
	return nil
}
func (m *mockRegistryProxy) Register(registry.RegisterOptions) (interface{}, error) { return nil, nil }

func (m *mockRegistryProxy) LoadByEnv(env string, reload bool) (*types.RegistryData, error) {
	if m.others != nil {
		if d, ok := m.others[strings.ToLower(env)]; ok {
			return d, nil
		}
		return nil, errors.New("no registry for " + env)
	}
	return m.Get(), nil
}
func (m *mockRegistryProxy) Close() {}

const testExtKey = "5bbf311a30f4acaec52b7472be854cc95259156275114822ad91615d02d27609ac1ebf7aecfdc2355849a594cc55d042d9906c2c0190a3b7d5f5935d8053a953ede02160a6f8429f1ba971b8d9d6fe6afe7c59af3462d188c4b1a08e9c07670e"

func boolPtr(b bool) *bool { return &b }

// proxyEnv is a service on a test server, the registry and awareness that point to it.
type proxyEnv struct {
	mon      *Monitor
	reg      *mockRegistryProxy
	aw       *mockAwarenessProxy
	port     int
	received atomic.Value
	body     atomic.Value
}

func newProxyEnv(t *testing.T, handler http.HandlerFunc) *proxyEnv {
	t.Helper()
	e := &proxyEnv{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		e.received.Store(r.Clone(r.Context()))
		e.body.Store(string(raw))
		if handler != nil {
			handler(w, r)
			return
		}
		w.Header().Set("X-Service", "yes")
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("from the service"))
	}))
	t.Cleanup(srv.Close)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	e.port, _ = strconv.Atoi(p)
	e.reg = &mockRegistryProxy{data: &types.RegistryData{
		ServiceConfig: &types.GlobalServiceConfig{
			Key:   types.KeyEncryptionConfig{Algorithm: "aes256", Password: "conformance fixture key"},
			Ports: types.PortsConfig{Controller: 4000, MaintenanceInc: 1000},
		},
		Services: map[string]*types.ServiceConfig{
			"controller": {Port: 4000, RequestTimeout: 30, RequestTimeoutRenewal: 0},
			"svc": {Port: e.port, RequestTimeout: 30, RequestTimeoutRenewal: 5, Hosts: &types.ServiceHosts{Latest: "2", ByVersion: map[string][]string{"2": {"127.0.0.1"}}},
				Versions: map[string]*types.VersionInfo{"1": {ExtKeyRequired: true}, "2": {}}},
			"oauth": {Port: e.port, Hosts: &types.ServiceHosts{Latest: "1", ByVersion: map[string][]string{"1": {"127.0.0.1"}}},
				Versions: map[string]*types.VersionInfo{"1": {}}},
		},
		CustomRegistry: map[string]interface{}{},
	}}
	e.aw = &mockAwarenessProxy{
		hosts: map[string]*types.HostInfo{
			"svc:2":   {Name: "svc", Version: "2", Host: "127.0.0.1", Port: e.port},
			"svc:1":   {Name: "svc", Version: "1", Host: "127.0.0.1", Port: e.port},
			"oauth:1": {Name: "oauth", Version: "1", Host: "127.0.0.1", Port: e.port},
		},
		latest: map[string]string{"svc": "2", "oauth": "1"},
	}
	return e
}

// serve runs URL, GotoService, InjectObj and the gotoservice, as the gateway's chain does.
func (e *proxyEnv) serve(req *http.Request) *httptest.ResponseRecorder {
	handler := Context(zap.NewNop(), "test")(URL(nil)(GotoService(e.reg, e.aw, e.monitor(), &mockProvision{}, GatewayService{Name: "controller", Version: "1"})(InjectObj(e.reg, e.aw)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := GetSOAJSContext(r.Context())
		if ctx.Controller.Gotoservice == nil {
			w.WriteHeader(299)
			return
		}
		ctx.Controller.Gotoservice(w, r)
	})))))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func (e *proxyEnv) monitor() *Monitor {
	if e.mon == nil {
		e.mon = NewMonitor(e.reg.Get, e.aw, zap.NewNop())
	}
	return e.mon
}

func (e *proxyEnv) got() *http.Request {
	r, _ := e.received.Load().(*http.Request)
	return r
}

func TestGotoService_Resolution(t *testing.T) {
	e := newProxyEnv(t, nil)
	cases := []struct {
		name, path string
		headers    map[string]string
		code       string
		status     int
	}{
		{"unknown service", "/nope/x", nil, `"code":130`, 0},
		{"unknown soajs route", "/soajs/other", nil, `"code":130`, 0},
		{"extKeyRequired without key", "/svc/v1/x", nil, `"code":132`, 0},
		{"extKeyRequired with a bad key", "/svc/v1/x", map[string]string{"key": "bad"}, `"code":132`, 0},
		{"soajs acl without a key", "/soajs/acl", nil, `"code":132`, 0},
		{"soajs acl with a key that did not resolve", "/soajs/acl", map[string]string{"key": "any"}, `"code":132`, 0},
		{"soajs proxy with a bad key", "/soajs/proxy?proxyRoute=%2Fsvc%2Fx", map[string]string{"key": "bad"}, `"code":132`, 0},
		{"soajs proxy to a relative route", "/soajs/proxy?proxyRoute=%2Fsvc%2Fx", nil, `"code":135`, 0},
		{"soajs proxy without a route", "/soajs/proxy", nil, `"code":139`, 0},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", c.path, nil)
		for k, v := range c.headers {
			req.Header.Set(k, v)
		}
		rec := e.serve(req)
		if c.status != 0 {
			if rec.Code != c.status {
				t.Errorf("%s: %d", c.name, rec.Code)
			}
			continue
		}
		if !strings.Contains(rec.Body.String(), c.code) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
	}

	// a key that decrypts passes
	req := httptest.NewRequest("GET", "/svc/v1/x?key="+testExtKey, nil)
	if rec := e.serve(req); rec.Code != http.StatusTeapot {
		t.Errorf("key in the query: %d %s", rec.Code, rec.Body.String())
	}
	// no service hosts outside HA
	e.reg.data.Services["svc"].Hosts = nil
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); !strings.Contains(rec.Body.String(), `"code":130`) {
		t.Errorf("no hosts: %s", rec.Body.String())
	}
	// no version known
	e.reg.data.Services["svc"].Hosts = &types.ServiceHosts{}
	e.aw.latest = map[string]string{}
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); !strings.Contains(rec.Body.String(), `"code":130`) {
		t.Errorf("no version: %s", rec.Body.String())
	}
}

func TestRedirect_PassesTheRequestAndTheAnswerAsTheyAre(t *testing.T) {
	e := newProxyEnv(t, nil)
	req := httptest.NewRequest("POST", "/svc/a%2Fb/c/?x=1&y=%20", strings.NewReader("payload"))
	req.Host = "gateway.example"
	req.Header.Set("X-Custom", "v")
	req.Header.Set("soajsinjectobj", `{"forged":true}`)
	rec := e.serve(req)
	if rec.Code != http.StatusTeapot || rec.Body.String() != "from the service" || rec.Header().Get("X-Service") != "yes" {
		t.Fatalf("%d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	got := e.got()
	if got.RequestURI != "/a%2Fb/c/?x=1&y=%20" {
		t.Errorf("raw path and query: %q", got.RequestURI)
	}
	if got.Host != "gateway.example" || got.Header.Get("X-Custom") != "v" || e.body.Load() != "payload" {
		t.Errorf("host %q headers %v body %v", got.Host, got.Header, e.body.Load())
	}
	if got.Header.Get("soajsinjectobj") != "" {
		t.Error("only the soajsinjectobj the gateway builds is sent")
	}

	// HEAD: the status and headers, no body
	rec = e.serve(httptest.NewRequest("HEAD", "/svc/x", nil))
	if rec.Code != http.StatusTeapot || rec.Body.Len() != 0 {
		t.Errorf("HEAD: %d %q", rec.Code, rec.Body.String())
	}
}

func TestRedirect_SimpleRTSAndErrors(t *testing.T) {
	e := newProxyEnv(t, nil)
	req := httptest.NewRequest("GET", "/oauth/passport/login/github", nil)
	req.Host = "gateway.example"
	if rec := e.serve(req); rec.Code != http.StatusTeapot {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if e.got().Host != "127.0.0.1" {
		t.Errorf("simpleRTS sets Host to the service's host: %q", e.got().Host)
	}

	// no host: 133; a service that does not answer: 135; no controller entry: 131
	delete(e.aw.hosts, "svc:2")
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); !strings.Contains(rec.Body.String(), `"code":133`) {
		t.Errorf("no host: %d %s", rec.Code, rec.Body.String())
	}
	e.aw.hosts["svc:2"] = &types.HostInfo{Host: "127.0.0.1"}
	e.reg.data.Services["svc"].Port = 1
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); !strings.Contains(rec.Body.String(), `"code":135`) {
		t.Errorf("refused: %d %s", rec.Code, rec.Body.String())
	}
	delete(e.reg.data.Services, "controller")
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); !strings.Contains(rec.Body.String(), `"code":131`) {
		t.Errorf("no controller: %d %s", rec.Code, rec.Body.String())
	}
}

// heartbeatServer answers /heartbeat with status, on the service's maintenance port.
func heartbeatServer(t *testing.T, e *proxyEnv, status int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	t.Cleanup(srv.Close)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	hb, _ := strconv.Atoi(p)
	e.reg.data.ServiceConfig.Ports.MaintenanceInc = hb - e.port
}

func TestRedirect_RequestTimeout(t *testing.T) {
	slow := func(d time.Duration) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-time.After(d):
				_, _ = w.Write([]byte("late"))
			case <-r.Context().Done():
			}
		}
	}
	// requestTimeout 1: an idle wait of 100 ms; requestTimeoutRenewal 3: 134 at the third timeout
	e := newProxyEnv(t, slow(5*time.Second))
	heartbeatServer(t, e, 200)
	e.reg.data.Services["svc"].RequestTimeout = 1
	e.reg.data.Services["svc"].RequestTimeoutRenewal = 3
	start := time.Now()
	rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil))
	if !strings.Contains(rec.Body.String(), `"code":134`) {
		t.Errorf("renewals exhausted: %d %s", rec.Code, rec.Body.String())
	}
	if d := time.Since(start); d < 250*time.Millisecond || d > 3*time.Second {
		t.Errorf("134 after %s, want about 300 ms", d)
	}

	// a heartbeat that fails: 133
	e = newProxyEnv(t, slow(5*time.Second))
	heartbeatServer(t, e, 500)
	e.reg.data.Services["svc"].RequestTimeout = 1
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); !strings.Contains(rec.Body.String(), `"code":133`) {
		t.Errorf("heartbeat down: %d %s", rec.Code, rec.Body.String())
	}

	// renewReqMonitorOff: no monitor
	e = newProxyEnv(t, slow(400*time.Millisecond))
	e.reg.data.Services["svc"].RequestTimeout = 1
	e.reg.data.CustomRegistry["gateway"] = map[string]interface{}{"value": map[string]interface{}{"gotoService": map[string]interface{}{"renewReqMonitorOff": true}}}
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); rec.Body.String() != "late" {
		t.Errorf("monitor off: %d %s", rec.Code, rec.Body.String())
	}

	// a stream that pauses longer than the idle wait is not cut while its service is healthy
	e = newProxyEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 4; i++ {
			_, _ = w.Write([]byte("tick\n"))
			w.(http.Flusher).Flush()
			time.Sleep(250 * time.Millisecond)
		}
	})
	heartbeatServer(t, e, 200)
	e.reg.data.Services["svc"].RequestTimeout = 1
	e.reg.data.Services["svc"].RequestTimeoutRenewal = 2
	if rec := e.serve(httptest.NewRequest("GET", "/svc/x", nil)); rec.Body.String() != "tick\ntick\ntick\ntick\n" {
		t.Errorf("stream: %q", rec.Body.String())
	}
}

func TestAuthorizeAndSessionCookie(t *testing.T) {
	// recorded from soajs.core security authorization.setCookie
	for _, c := range [][2]string{
		{"Basic c29hanM6QzAxMDEyYjQ1NmY4OWEzY2RlN2doaWprbG1ub3BxcnN0dXY=", "soajsID=s:0123456789abcdefghijklmnopqrstuv.zbXaiV5R4Htro583InOaaXFzN6lBOMjHxCWxFtDQIxo"},
		{"Basic c29hanM6QzAyMDEyM2M1Njc4ZmFiNGRlOWdoaWprbG1ub3BxcnN0dXY=", "soajsID=s:0123456789abcdefghijklmnopqrstuv.zbXaiV5R4Htro583InOaaXFzN6lBOMjHxCWxFtDQIxo"},
		{"Basic c29hanM6QzAzMDEyMzRkNjc4OWFmYzVlYmdoaWprbG1ub3BxcnN0dXY=", "soajsID=s:0123456789abcdefghijklmnopqrstuv.zbXaiV5R4Htro583InOaaXFzN6lBOMjHxCWxFtDQIxo"},
		{"Basic c29hanM6WFlac2hvcnQ=", "soajsID=s:short.yqkCWTXpnH9W5X480CotBJ0BHTrAYvjJNujJDv24+kM"},
		{"Bearer x", ""},
		{"Basic bm90c29hanM=", ""},
	} {
		if got := sessionCookie(c[0], "s3cret", "soajsID"); got != c[1] {
			t.Errorf("%s: %q, want %q", c[0], got, c[1])
		}
	}
	if _, ok := umask("C01short"); ok {
		t.Error("a known style needs a 32-character id")
	}

	e := newProxyEnv(t, nil)
	e.reg.data.Services["controller"].Authorization = boolPtr(true)
	e.reg.data.ServiceConfig.Session = types.SessionConfig{Name: "soajsID", Secret: "s3cret"}
	req := httptest.NewRequest("GET", "/svc/x", nil)
	req.Header.Set("Cookie", "a=1")
	req.Header.Set("soajsauth", "Basic c29hanM6WFlac2hvcnQ=")
	e.serve(req)
	if got := e.got().Header.Get("Cookie"); got != "a=1;soajsID=s:short.yqkCWTXpnH9W5X480CotBJ0BHTrAYvjJNujJDv24+kM" {
		t.Errorf("cookie %q", got)
	}
	e.serve(httptest.NewRequest("GET", "/svc/x?soajsauth=Basic%20c29hanM6WFlac2hvcnQ%3D", nil))
	if got := e.got().Header.Get("Cookie"); got != ";soajsID=s:short.yqkCWTXpnH9W5X480CotBJ0BHTrAYvjJNujJDv24+kM" {
		t.Errorf("from the query, without a cookie: %q", got)
	}
}

func TestRoaming(t *testing.T) {
	e := newProxyEnv(t, nil)
	req := func() *http.Request {
		r := httptest.NewRequest("GET", "/svc/x", nil)
		r.Header.Set("soajsroaming", "1")
		r.RemoteAddr = "10.0.0.9:1234"
		return r
	}
	if rec := e.serve(req()); !strings.Contains(rec.Body.String(), `"code":165`) {
		t.Errorf("not configured: %d %s", rec.Code, rec.Body.String())
	}
	e.reg.data.CustomRegistry["oauth"] = map[string]interface{}{"value": map[string]interface{}{"roaming": map[string]interface{}{
		"services": map[string]interface{}{"svc": map[string]interface{}{"whitelistips": []interface{}{"10.0.0.9"}}},
	}}}
	if rec := e.serve(req()); rec.Code != 200 || rec.Body.String() != `{"result":true,"data":true}` {
		t.Errorf("whitelisted: %d %s", rec.Code, rec.Body.String())
	}
	r := req()
	r.RemoteAddr = "10.0.0.8:1234"
	if rec := e.serve(r); !strings.Contains(rec.Body.String(), `"code":165`) {
		t.Errorf("other IP: %d %s", rec.Code, rec.Body.String())
	}
}

func TestEncodeURIComponent(t *testing.T) {
	// recorded from encodeURIComponent in Node.js
	if got := encodeURIComponent("Jean-Luc Picard é ✓ !*()~ & %"); got != "Jean-Luc%20Picard%20%C3%A9%20%E2%9C%93%20!*()~%20%26%20%25" {
		t.Errorf("%q", got)
	}
	if encodeHeaderValue(nil) != "" {
		t.Error("none is empty")
	}
}

func injectCtx() *SOAJSContext {
	params := &types.ServiceParams{
		URLInfo: &types.URLInfo{ServiceName: "svc", ServiceVersion: "2"},
		KeyAPI:  true,
		Package: &types.Package{ACL: map[string]interface{}{"svc": map[string]interface{}{}}, ACLAllEnv: map[string]interface{}{"dev": map[string]interface{}{}}},
	}
	key := &types.Key{
		InternalKey: "ik", ExtKey: "ek",
		Tenant:      types.KeyTenant{ID: "t1", Code: "TC", Name: "Tenant", Type: "product", Profile: map[string]interface{}{"p": 1}},
		Application: types.KeyApplication{Product: "PROD", Package: "PROD_PACK", AppID: "app"},
		Config:      map[string]interface{}{"svc": map[string]interface{}{"a": 1}, "peer": map[string]interface{}{"b": 2}, "commonFields": map[string]interface{}{"c": 3}},
	}
	return &SOAJSContext{Log: zap.NewNop(), Key: key, ServicesConfig: key.Config, Controller: &ControllerContext{ServiceParams: params}}
}

func TestBuildInjectObj(t *testing.T) {
	e := newProxyEnv(t, nil)
	e.aw.hosts["controller:"] = &types.HostInfo{Host: "10.0.0.1"}
	ctx := injectCtx()
	h, ok := BuildInjectObj(ctx, e.reg, e.aw, &httpRequestInfo{userAgent: "curl/8", clientIP: "1.2.3.4"})
	if !ok {
		t.Fatal("too large")
	}
	want := `{"tenant":{"id":"t1","name":"Tenant","code":"TC","locked":false,"type":"product"},` +
		`"key":{"config":{"c":3,"svc":{"a":1}},"iKey":"ik","eKey":"ek"},` +
		`"application":{"product":"PROD","package":"PROD_PACK","appId":"app"},"package":{},` +
		`"device":"curl/8","geo":{"ip":"1.2.3.4"},"awareness":{"host":"10.0.0.1","port":4000}}`
	if h != want {
		t.Errorf("\n got  %s\n want %s", h, want)
	}

	// provision_ACL, tenant_Profile, interConnect (the peer's config joins key.config)
	e.reg.data.Services["svc"].Versions["2"] = &types.VersionInfo{ProvisionACL: true, TenantProfile: true,
		InterConnect: []types.InterConnectItem{{Name: "oauth", Version: "1"}, {Name: "missing"}, {Name: "oauth"}}}
	ctx.ServicesConfig["oauth"] = map[string]interface{}{"o": 1}
	ctx.Controller.ServiceParams.SkipACL = true
	h, _ = BuildInjectObj(ctx, e.reg, e.aw, &httpRequestInfo{userAgent: "curl/8", clientIP: "1.2.3.4"})
	var got map[string]interface{}
	if err := json.Unmarshal([]byte(h), &got); err != nil {
		t.Fatal(err)
	}
	if got["tenant"].(map[string]interface{})["profile"] == nil || got["package"].(map[string]interface{})["acl"] == nil {
		t.Errorf("profile and ACL: %s", h)
	}
	if _, ok := got["device"]; ok {
		t.Error("no device or geo when the ACL is skipped")
	}
	peers := got["awareness"].(map[string]interface{})["interConnect"].([]interface{})
	if len(peers) != 1 || peers[0].(map[string]interface{})["latest"] != "1" || peers[0].(map[string]interface{})["port"] != float64(e.port) {
		t.Errorf("interConnect: oauth@1 only, oauth latest has no host here: %v", peers)
	}
	if got["key"].(map[string]interface{})["config"].(map[string]interface{})["oauth"] == nil {
		t.Error("the peer's config joins key.config")
	}

	// without a controller host: no awareness, no interConnect
	delete(e.aw.hosts, "controller:")
	h, _ = BuildInjectObj(ctx, e.reg, e.aw, &httpRequestInfo{})
	if strings.Contains(h, "awareness") {
		t.Errorf("no awareness: %s", h)
	}

	// the size limit
	t.Setenv("SOAJS_MAX_INJECT_HEADER_SIZE", "50")
	if _, ok := BuildInjectObj(ctx, e.reg, e.aw, &httpRequestInfo{}); ok {
		t.Error("larger than the limit")
	}
}

// No middleware after Context sees a soajsinjectobj the request came with.
func TestContext_RemovesInboundInjectObj(t *testing.T) {
	seen := "unset"
	handler := Context(zap.NewNop(), "test")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("soajsinjectobj")
	}))
	req := httptest.NewRequest("GET", "/svc/x", nil)
	req.Header.Set("soajsinjectobj", `{"tenant":{"id":"sent"}}`)
	req.Header.Set("key", "k")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if seen != "" {
		t.Errorf("a middleware after Context saw %q", seen)
	}
	if req.Header.Get("key") != "k" {
		t.Error("other headers are kept")
	}
}
