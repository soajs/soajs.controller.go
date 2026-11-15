package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/soajs/soajs.controller.go/internal/events"
	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/pkg/types"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// captureSink keeps the events it is given.
type captureSink struct {
	mu  sync.Mutex
	got []*events.Event
}

func (s *captureSink) Write(_ context.Context, batch []*events.Event) error {
	s.mu.Lock()
	s.got = append(s.got, batch...)
	s.mu.Unlock()
	return nil
}

func (s *captureSink) events() []*events.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*events.Event(nil), s.got...)
}

const eventsSecretKey = "5bbf311a30f4acaec52b7472be854cc9secret"

// eventsEnv is CallEvents with a capture sink, in front of a handler set by each test; prepare
// sets the request context as the earlier middlewares would.
type eventsEnv struct {
	reg  *mockRegistryProxy
	ce   *CallEvents
	sink *captureSink
}

func newEventsEnv(config map[string]interface{}) *eventsEnv {
	reg := &mockRegistryProxy{data: &types.RegistryData{CustomRegistry: map[string]interface{}{}}}
	if config != nil {
		reg.data.CustomRegistry["gateway"] = map[string]interface{}{"value": map[string]interface{}{"events": config}}
	}
	sink := &captureSink{}
	ce := NewCallEvents(reg, func(string) (*drv.Database, error) { return nil, nil }, GatewayService{Name: "controller", Version: "1"}, "DEV", zap.NewNop())
	ce.testSink = sink
	return &eventsEnv{reg: reg, ce: ce, sink: sink}
}

func (e *eventsEnv) serve(req *http.Request, prepare func(*SOAJSContext), handler http.HandlerFunc) *httptest.ResponseRecorder {
	h := chimw.RequestID(Context(zap.NewNop(), "dev")(URL(nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if prepare != nil {
			prepare(GetSOAJSContext(r.Context()))
		}
		e.ce.Middleware()(handler).ServeHTTP(w, r)
	}))))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// flush writes the queued events.
func (e *eventsEnv) flush() []*events.Event {
	if em := e.ce.Emitter(); em != nil {
		em.Close()
	}
	return e.sink.events()
}

func orderACL(t *testing.T) *types.FinalACL {
	re, err := lib.PathToRegexp("/orders/:id")
	if err != nil {
		t.Fatal(err)
	}
	return &types.FinalACL{APIsRegExp: []*types.ACLAPI{{Route: "/orders/:id", RegExp: re, Access: true}}}
}

func TestCallEvents_OffChangesNothing(t *testing.T) {
	for _, config := range []map[string]interface{}{nil, {"enabled": false, "sink": "log"}} {
		e := newEventsEnv(config)
		var seen http.ResponseWriter
		rec := e.serve(httptest.NewRequest("GET", "/orders/v1/orders/7", nil), nil, func(w http.ResponseWriter, r *http.Request) {
			seen = w
			w.WriteHeader(204)
		})
		if _, wrapped := seen.(*eventWriter); wrapped || rec.Code != 204 || e.ce.Emitter() != nil {
			t.Errorf("events off (%v): wrapped %v, emitter %v", config, wrapped, e.ce.Emitter())
		}
	}
}

func TestCallEvents_Fields(t *testing.T) {
	e := newEventsEnv(map[string]interface{}{"enabled": true, "sink": "log"})
	req := httptest.NewRequest("GET", "/orders/v1/orders/7?secret=qvalue&access_token=tok123", nil)
	req.Header.Set("key", eventsSecretKey)
	req.Header.Set("Authorization", "Bearer tok456")
	req.Header.Set("X-Forwarded-For", "10.1.2.3")
	key := mtKey(eventsSecretKey, "P_PKG")
	key.Application.AppID = "app1"
	key.Tenant.Main = &types.TenantRef{ID: "main-1", Code: "MAIN"}
	rec := e.serve(req, func(c *SOAJSContext) {
		c.Key = key
		c.Tenant = types.TenantFromKey(key)
		c.Controller.ServiceParams.ACL = orderACL(t)
		c.Controller.ServiceParams.OAuthToken = &types.AccessToken{Token: "tok456", User: map[string]interface{}{
			"id": "u1", "username": "agent1", "restrictedTo": map[string]interface{}{"tenant": "tenant-123", "key": eventsSecretKey}}}
		c.Marks.Cache = "HIT"
	}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{"secret":"body"}`))
	})
	if rec.Code != 200 {
		t.Fatalf("answer %d", rec.Code)
	}
	got := e.flush()
	if len(got) != 1 {
		t.Fatalf("%d events", len(got))
	}
	ev := got[0]
	if ev.API != "/orders/:id" || ev.Service != "orders" || ev.Version != "1" || ev.Method != "GET" || ev.Status != 200 || ev.Code != 0 {
		t.Errorf("routing %+v", ev)
	}
	if ev.Env != "dev" || ev.RequestID == "" || ev.IP != "10.1.2.3" || ev.Cache != "HIT" || ev.TS.IsZero() || ev.DurationMs < 0 {
		t.Errorf("context %+v", ev)
	}
	if ev.Tenant == nil || ev.Tenant.ID != "tenant-123" || ev.Tenant.Code != "MYTEN" || ev.Tenant.Main == nil || ev.Tenant.Main.Code != "MAIN" {
		t.Errorf("tenant %+v", ev.Tenant)
	}
	if ev.Product != "PROD" || ev.Package != "P_PKG" || ev.AppID != "app1" {
		t.Errorf("key %+v", ev)
	}
	if ev.User == nil || ev.User.ID != "u1" || ev.User.Username != "agent1" || !ev.User.Agent {
		t.Errorf("user %+v", ev.User)
	}
	raw, _ := json.Marshal(ev)
	for _, secret := range []string{eventsSecretKey, "tok123", "tok456", "qvalue", "secret", "body", "Bearer"} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the event carries %q: %s", secret, raw)
		}
	}
}

// A refusal records its status and code; a path no ACL route matches has no api.
func TestCallEvents_Refusal(t *testing.T) {
	e := newEventsEnv(map[string]interface{}{"enabled": true, "sink": "log"})
	e.serve(httptest.NewRequest("POST", "/orders/v1/elsewhere", nil), func(c *SOAJSContext) {
		c.Controller.ServiceParams.ACL = orderACL(t)
	}, func(w http.ResponseWriter, r *http.Request) {
		WriteCode(w, 154)
	})
	got := e.flush()
	if len(got) != 1 || got[0].Status != 401 || got[0].Code != 154 || got[0].API != "" || got[0].Tenant != nil || got[0].User != nil {
		t.Errorf("refusal %+v", *got[0])
	}
}

// The gateway's own routes: /soajs/acl and /soajs/mcp as the gateway's APIs, /soajs/proxy too.
func TestCallEvents_GatewayRoutes(t *testing.T) {
	e := newEventsEnv(map[string]interface{}{"enabled": true, "sink": "log"})
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	e.serve(httptest.NewRequest("POST", "/soajs/mcp", nil), func(c *SOAJSContext) {
		mimicGatewayAPI(c.Controller.ServiceParams, GatewayService{Name: "controller", Version: "1"}, "/soajs/mcp")
		c.Marks.MCPTool = "orders_getOrder"
	}, ok)
	e.serve(httptest.NewRequest("GET", "/soajs/proxy?proxyRoute=%2Fx", nil), func(c *SOAJSContext) {
		c.Controller.ServiceParams.Proxy = true
		c.Controller.ServiceParams.URLInfo.ServiceName = "x"
	}, ok)
	got := e.flush()
	if len(got) != 2 {
		t.Fatalf("%d events", len(got))
	}
	if got[0].Service != "controller" || got[0].API != "/soajs/mcp" || got[0].MCPTool != "orders_getOrder" {
		t.Errorf("mcp %+v", got[0])
	}
	if got[1].Service != "controller" || got[1].API != "/soajs/proxy" {
		t.Errorf("proxy %+v", got[1])
	}
}

// With services, only those enabled emit.
func TestCallEvents_ServicesFilter(t *testing.T) {
	e := newEventsEnv(map[string]interface{}{"enabled": true, "sink": "log",
		"services": map[string]interface{}{"orders": map[string]interface{}{"enabled": true}, "other": map[string]interface{}{"enabled": false}}})
	ok := func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }
	for _, p := range []string{"/orders/x", "/other/x", "/third/x"} {
		e.serve(httptest.NewRequest("GET", p, nil), nil, ok)
	}
	got := e.flush()
	if len(got) != 1 || got[0].Service != "orders" {
		t.Errorf("filtered %+v", got)
	}
}

// The code of an answer the request monitor ends after the headers is recorded too.
func TestCallEvents_NoteCodeThroughWrappers(t *testing.T) {
	ew := &eventWriter{ResponseWriter: httptest.NewRecorder()}
	outer := &eventsWrap{ew}
	noteCode(outer, 133)
	noteCode(outer, 135)
	if _, code := ew.result(); code != 133 {
		t.Errorf("code %d", code)
	}
	noteCode(httptest.NewRecorder(), 1)
}

type eventsWrap struct{ w http.ResponseWriter }

func (e *eventsWrap) Header() http.Header         { return e.w.Header() }
func (e *eventsWrap) Write(b []byte) (int, error) { return e.w.Write(b) }
func (e *eventsWrap) WriteHeader(s int)           { e.w.WriteHeader(s) }
func (e *eventsWrap) Unwrap() http.ResponseWriter { return e.w }

// Many requests at once (go test -race): one event each.
func TestCallEvents_Concurrent(t *testing.T) {
	e := newEventsEnv(map[string]interface{}{"enabled": true, "sink": "log", "queueSize": 1000.0})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.serve(httptest.NewRequest("GET", "/orders/x", nil), nil, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(200)
			})
		}()
	}
	wg.Wait()
	time.Sleep(10 * time.Millisecond)
	if got := e.flush(); len(got) != 50 {
		t.Errorf("%d events", len(got))
	}
}
