package middleware

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/urac"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

func throttlingRegistry(custom map[string]interface{}) *mockRegistryProxy {
	return &mockRegistryProxy{data: &types.RegistryData{
		ServiceConfig: &types.GlobalServiceConfig{Throttling: map[string]interface{}{
			"publicAPIStrategy":  "pub",
			"privateAPIStrategy": "priv",
			"pub":                map[string]interface{}{"type": 1.0, "window": 60000.0, "limit": 2.0, "retries": 0.0, "delay": 10.0},
			"priv":               map[string]interface{}{"type": 0.0, "window": 60000.0, "limit": 1.0, "retries": 1.0, "delay": 20.0},
			"tight":              map[string]interface{}{"type": 1.0, "window": 60000.0, "limit": 0.0, "retries": 0.0, "delay": 0.0},
		}},
		CustomRegistry: custom,
	}}
}

// runTraffic sends a request of tenant t1 from ip through Traffic.
func runTraffic(t *testing.T, reg *mockRegistryProxy, models map[string]ThrottlingModel, public bool, ip, service, path string, servicesConfig map[string]interface{}) *httptest.ResponseRecorder {
	t.Helper()
	setup := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := GetSOAJSContext(r.Context())
			ctx.Tenant = &types.Tenant{ID: "t1"}
			ctx.ServicesConfig = servicesConfig
			ctx.Controller.ServiceParams.URLInfo = &types.URLInfo{ServiceName: service, ServicePath: path}
			ctx.Controller.ServiceParams.IsAPIPublic = public
			next.ServeHTTP(w, r)
		})
	}
	handler := Context(zap.NewNop(), "test")(setup(Traffic(reg, models, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(299)
	}))))
	req := httptest.NewRequest("GET", "/"+service+path, nil)
	req.RemoteAddr = ip + ":1000"
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func memoryModels() map[string]ThrottlingModel {
	return map[string]ThrottlingModel{"memory": NewMemoryThrottling(context.Background(), zap.NewNop())}
}

func TestTraffic_PublicStrategyByIP(t *testing.T) {
	reg := throttlingRegistry(nil)
	models := memoryModels()
	for i := 0; i < 2; i++ {
		if rec := runTraffic(t, reg, models, true, "1.1.1.1", "svc", "/a", nil); rec.Code != 299 {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	rec := runTraffic(t, reg, models, true, "1.1.1.1", "svc", "/a", nil)
	if rec.Code != 429 || rec.Body.String() != `{"status":429,"msg":"too many requests"}` {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-RateLimit-Limit") != "2" || rec.Header().Get("X-RateLimit-Remaining") != "-1" {
		t.Errorf("headers %v", rec.Header())
	}
	if ra, err := strconv.ParseFloat(rec.Header().Get("Retry-After"), 64); err != nil || ra <= 59 || ra > 60 {
		t.Errorf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	// another IP of the tenant has its own counter (type 1)
	if rec := runTraffic(t, reg, models, true, "2.2.2.2", "svc", "/a", nil); rec.Code != 299 {
		t.Errorf("other IP: %d", rec.Code)
	}
	// no rate-limit headers when the request passes
	if rec := runTraffic(t, reg, models, true, "3.3.3.3", "svc", "/a", nil); rec.Header().Get("X-RateLimit-Limit") != "" {
		t.Error("headers only on 429")
	}
}

func TestTraffic_PrivateStrategyByTenantWithRetries(t *testing.T) {
	reg := throttlingRegistry(nil)
	models := memoryModels()
	if rec := runTraffic(t, reg, models, false, "1.1.1.1", "svc", "/a", nil); rec.Code != 299 {
		t.Fatal(rec.Code)
	}
	start := time.Now()
	// type 0: by tenant, whatever the IP; one retry after 20 ms, then 429
	if rec := runTraffic(t, reg, models, false, "9.9.9.9", "svc", "/a", nil); rec.Code != 429 {
		t.Errorf("tenant counter: %d", rec.Code)
	}
	if time.Since(start) < 20*time.Millisecond {
		t.Error("checked again after delay")
	}
}

func TestTraffic_StrategyOverrides(t *testing.T) {
	custom := map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"traffic": map[string]interface{}{
		"throttling": map[string]interface{}{
			"svc":   map[string]interface{}{"publicAPIStrategy": "tight", "apis": []interface{}{"/login"}},
			"other": map[string]interface{}{"publicAPIStrategy": nil},
		},
	}}}}
	reg := throttlingRegistry(custom)
	models := memoryModels()
	if rec := runTraffic(t, reg, models, true, "1.1.1.1", "svc", "/login", nil); rec.Code != 429 {
		t.Errorf("the custom registry strategy for the listed api: %d", rec.Code)
	}
	if rec := runTraffic(t, reg, models, true, "1.1.1.1", "svc", "/other", nil); rec.Code != 299 {
		t.Errorf("an api not listed keeps the default: %d", rec.Code)
	}
	for i := 0; i < 5; i++ {
		if rec := runTraffic(t, reg, models, true, "1.1.1.1", "other", "/x", nil); rec.Code != 299 {
			t.Fatalf("a null strategy turns throttling off: %d", rec.Code)
		}
	}
	// the key's gateway.throttling comes before the custom registry
	keyConfig := map[string]interface{}{"gateway": map[string]interface{}{"throttling": map[string]interface{}{
		"svc": map[string]interface{}{"publicAPIStrategy": nil},
	}}}
	for i := 0; i < 5; i++ {
		if rec := runTraffic(t, reg, models, true, "4.4.4.4", "svc", "/login", keyConfig); rec.Code != 299 {
			t.Fatalf("the key's throttling first: %d", rec.Code)
		}
	}
}

func TestTraffic_Model(t *testing.T) {
	custom := map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"traffic": map[string]interface{}{"model": "redis"}}}}
	reg := throttlingRegistry(custom)
	for i := 0; i < 5; i++ {
		if rec := runTraffic(t, reg, memoryModels(), true, "1.1.1.1", "svc", "/a", nil); rec.Code != 299 {
			t.Fatalf("an unknown model turns throttling off: %d", rec.Code)
		}
	}
	// no tenant (not a key's API), no registry throttling: passes
	reg = &mockRegistryProxy{data: &types.RegistryData{ServiceConfig: &types.GlobalServiceConfig{}}}
	if rec := runTraffic(t, reg, memoryModels(), true, "1.1.1.1", "svc", "/a", nil); rec.Code != 299 {
		t.Errorf("no throttling: %d", rec.Code)
	}
}

func TestMemoryThrottling_WindowAndCleanup(t *testing.T) {
	m := NewMemoryThrottling(context.Background(), zap.NewNop())
	now := time.Now()
	m.now = func() time.Time { return now }
	s := throttlingStrategy{perIP: true, window: 1000, limit: 1}
	k := trafficKey{"t1", "ip"}
	m.Increment(s, k)
	if m.Get(s, k).Count != 1 {
		t.Fatal("counted")
	}
	if m.Get(throttlingStrategy{window: 1000}, k).Count != 0 {
		t.Error("the tenant counter is separate from the IP counter")
	}
	now = now.Add(2 * time.Hour)
	m.Cleanup()
	if len(m.tenants) != 0 {
		t.Error("counters older than an hour are dropped")
	}
	// an expired window starts over
	m.now = time.Now
	m.Reset(s, k)
	m.Increment(s, k)
	m.mu.Lock()
	m.tenants["t1"].ips["ip"].FirstReqTime = time.Now().Add(-2 * time.Second)
	m.mu.Unlock()
	if _, ok := checkThrottling(context.Background(), m, s, "x", k); !ok {
		t.Error("an expired window starts over")
	}
}

// listenerServer is a test server; it returns its handler's calls.
func listenerServer(t *testing.T) (int, func() []*http.Request, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var reqs []*http.Request
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, r.Clone(r.Context()))
		bodies = append(bodies, string(raw))
		mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	_, p, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	return port, func() []*http.Request {
			mu.Lock()
			defer mu.Unlock()
			return append([]*http.Request(nil), reqs...)
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), bodies...)
		}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLastSeenIncludes(t *testing.T) {
	include := map[string]interface{}{
		"all":    true,
		"spaces": map[string]interface{}{"apis": map[string]interface{}{"/active": []interface{}{"get"}, "/space/:id": true}},
		"wild":   map[string]interface{}{"apis": map[string]interface{}{"*": []interface{}{"POST"}}},
		"full":   map[string]interface{}{"apis": map[string]interface{}{"/full/x": true}},
	}
	cases := []struct {
		service, api, full, method string
		want                       bool
	}{
		{"all", "/x", "/all/x", "DELETE", true},
		{"spaces", "/active", "/spaces/active", "GET", true},
		{"spaces", "/active", "/spaces/active", "POST", false},
		{"spaces", "/space/12", "/spaces/space/12", "PUT", true},
		{"wild", "/any", "/wild/any", "post", true},
		{"wild", "/any", "/wild/any", "GET", false},
		{"full", "/x", "/full/x", "GET", true},
		{"none", "/x", "/none/x", "GET", false},
	}
	for _, c := range cases {
		if got := lastSeenIncludes(include, c.service, c.api, c.full, c.method); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

func TestLastSeen_Notifies(t *testing.T) {
	port, calls, bodies := listenerServer(t)
	reg := &mockRegistryProxy{data: &types.RegistryData{
		Services: map[string]*types.ServiceConfig{"urac": {Port: port}, "audit": {Port: port}},
		CustomRegistry: map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"lastSeen": map[string]interface{}{
			"active": true, "network": "net1",
			"targets": []interface{}{map[string]interface{}{"serviceName": "audit", "api": "/seen", "method": "PUT"}, map[string]interface{}{"api": "/no-service"}},
		}}}},
	}}
	aw := &mockAwarenessProxy{hosts: map[string]*types.HostInfo{
		"urac:3": {Host: "127.0.0.1"},
		"audit:": {Host: "127.0.0.1"},
	}}
	user := uracUserWithProfile(t)
	setup := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := GetSOAJSContext(r.Context())
			ctx.Urac = user
			ctx.Controller.ServiceParams.URLInfo = &types.URLInfo{ServiceName: "svc", ServicePath: "/x"}
			r.Header.Set("soajsinjectobj", `{"built":true}`)
			next.ServeHTTP(w, r)
		})
	}
	handler := Context(zap.NewNop(), "test")(setup(LastSeen(reg, aw)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/svc/x", nil))
	waitFor(t, func() bool { return len(calls()) == 2 })
	byPath := map[string]*http.Request{}
	for _, c := range calls() {
		byPath[c.URL.Path] = c
	}
	if c := byPath["/user/last/seen"]; c == nil || c.Method != "POST" || c.Header.Get("soajsinjectobj") != `{"built":true}` || c.Header.Get("Content-Type") != "application/json" {
		t.Errorf("urac notification: %+v", c)
	}
	if c := byPath["/seen"]; c == nil || c.Method != "PUT" {
		t.Errorf("target: %+v", c)
	}
	for _, b := range bodies() {
		if b != `{"network":"net1"}` {
			t.Errorf("body %q", b)
		}
	}
}

func TestMonitor_RecordsAndSends(t *testing.T) {
	monPort, calls, bodies := listenerServer(t)
	e := newProxyEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	e.reg.data.Services["soamonitor"] = &types.ServiceConfig{Port: monPort}
	e.aw.hosts["soamonitor:1"] = &types.HostInfo{Host: "127.0.0.1"}
	e.reg.data.CustomRegistry["gateway"] = map[string]interface{}{"value": map[string]interface{}{"gotoService": map[string]interface{}{
		"monitor": map[string]interface{}{"req_info": true, "req_header": true, "req_query": true, "req_body": true, "req_response": true},
	}}}
	req := httptest.NewRequest("POST", "/svc/x?a=1", strings.NewReader(`{"in":1}`))
	req.Header.Set("Content-Type", "application/json")
	if rec := e.serve(req); rec.Body.String() != `{"ok":true}` {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	waitFor(t, func() bool { return len(calls()) == 1 })
	if calls()[0].URL.Path != "/monitor/item" {
		t.Errorf("path %s", calls()[0].URL.Path)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(bodies()[0]), &doc); err != nil {
		t.Fatal(err)
	}
	times := doc["time"].(map[string]interface{})
	for _, k := range []string{"req_start", "req_body_start", "req_body_end", "res_start", "res_end"} {
		if times[k] == nil {
			t.Errorf("time.%s missing: %v", k, times)
		}
	}
	if doc["name"] != "svc" || doc["method"] != "POST" || doc["url"] != "/x?a=1" || doc["host"] != "127.0.0.1" ||
		doc["body"] != `{"in":1}` || doc["response"] != `{"ok":true}` || doc["query"].(map[string]interface{})["a"] != "1" {
		t.Errorf("doc %v", doc)
	}
	if !strings.HasPrefix(bodies()[0], `{"time":{"req_start":`) {
		t.Errorf("field order: %s", bodies()[0])
	}

	// without req_response nothing is sent; a blacklisted service is not monitored
	e.reg.data.CustomRegistry["gateway"] = map[string]interface{}{"value": map[string]interface{}{"gotoService": map[string]interface{}{
		"monitor": map[string]interface{}{"req_info": true},
	}}}
	e.serve(httptest.NewRequest("GET", "/svc/x", nil))
	e.reg.data.CustomRegistry["gateway"] = map[string]interface{}{"value": map[string]interface{}{"gotoService": map[string]interface{}{
		"monitor": map[string]interface{}{"req_response": true, "blacklist": []interface{}{"svc"}},
	}}}
	e.serve(httptest.NewRequest("GET", "/svc/x", nil))
	time.Sleep(100 * time.Millisecond)
	if len(calls()) != 1 {
		t.Errorf("sent %d", len(calls()))
	}

	// buffered: a batch at the limit
	e.reg.data.CustomRegistry["gateway"] = map[string]interface{}{"value": map[string]interface{}{"gotoService": map[string]interface{}{
		"monitor": map[string]interface{}{"req_response": true, "buffer": map[string]interface{}{"limit": 2.0}},
	}}}
	e.serve(httptest.NewRequest("GET", "/svc/x", nil))
	e.serve(httptest.NewRequest("GET", "/svc/x", nil))
	waitFor(t, func() bool { return len(calls()) == 2 })
	if calls()[1].URL.Path != "/monitor/items" || !strings.HasPrefix(bodies()[1], `{"items":[`) {
		t.Errorf("batch %s %s", calls()[1].URL.Path, bodies()[1])
	}
}

// uracUserWithProfile is a logged-in user whose record is the token's (loginMode oauth).
func uracUserWithProfile(t *testing.T) *urac.User {
	t.Helper()
	u := urac.NewUser(urac.NewDriverWithStore(nil, zap.NewNop()), &urac.Request{
		Token:  &types.AccessToken{User: map[string]interface{}{"id": "u1", "_id": "u1", "username": "user", "loginMode": "oauth"}},
		Logger: zap.NewNop(),
	})
	if _, err := u.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return u
}
