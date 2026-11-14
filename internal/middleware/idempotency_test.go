package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

const (
	idemKey1 = "3f1c2a9e-8b7d-4c6e-9a5f-1d2e3f4a5b6c"
	idemKey2 = "9B2D4F6A-1C3E-4A5B-8D7F-0E1F2A3B4C5D"
)

func TestMatchesIdempotentAPI(t *testing.T) {
	apis := []interface{}{"POST /orders", "PUT /orders/:id/items/:item", 42}
	cases := []struct {
		method, path string
		want         bool
	}{
		{"POST", "/orders", true},
		{"PUT", "/orders/7/items/x", true},
		{"PUT", "/orders/7/items", false},
		{"POST", "/orders/7", false},
		{"PATCH", "/orders", false},
	}
	for _, c := range cases {
		if got := matchesIdempotentAPI(c.method, c.path, apis); got != c.want {
			t.Errorf("%s %s: %v", c.method, c.path, got)
		}
	}
}

// idemEnv is a gateway with the idempotency middleware on a service redirect.
type idemEnv struct {
	e     *proxyEnv
	model *MemoryIdempotency
	calls atomic.Int32
	// status is what the service answers
	status atomic.Int32
}

func newIdemEnv(t *testing.T, config map[string]interface{}) *idemEnv {
	ie := &idemEnv{}
	ie.status.Store(http.StatusCreated)
	ie.e = newProxyEnv(t, func(w http.ResponseWriter, r *http.Request) {
		n := ie.calls.Add(1)
		w.Header().Set("Set-Cookie", "a=1")
		w.Header().Add("Set-Cookie", "b=2")
		w.Header().Set("X-Call", strings.Repeat("x", int(n)))
		w.WriteHeader(int(ie.status.Load()))
		_, _ = w.Write([]byte("answer\xff"))
	})
	ie.e.reg.data.CustomRegistry = map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"idempotency": config}}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ie.model = NewMemoryIdempotency(ctx)
	return ie
}

func (ie *idemEnv) serve(req *http.Request, tenant bool) *httptest.ResponseRecorder {
	e := ie.e
	handler := Context(zap.NewNop(), "test")(URL(nil)(GotoService(e.reg, e.aw, e.monitor(), &mockProvision{}, GatewayService{Name: "controller", Version: "1"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if tenant {
				GetSOAJSContext(r.Context()).Tenant = &types.Tenant{ID: "t1"}
			}
			Idempotency(e.reg, map[string]IdempotencyModel{"memory": ie.model}, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				GetSOAJSContext(r.Context()).Controller.Gotoservice(w, r)
			})).ServeHTTP(w, r)
		}))))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func idemRequest(method, path, key string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader("{}"))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	return req
}

func TestIdempotency_Decisions(t *testing.T) {
	ie := newIdemEnv(t, map[string]interface{}{
		"model": "memory",
		"services": map[string]interface{}{
			"svc": map[string]interface{}{"enabled": true, "enforce": true, "apis": []interface{}{"POST /orders", "PUT /orders/:id"}},
		},
	})
	cases := []struct {
		name      string
		req       *http.Request
		tenant    bool
		wantCode  string
		wantCalls int32
	}{
		{"an invalid key, without a tenant", idemRequest("GET", "/svc/v2/x", "nope"), false, `"codes":[180]`, 0},
		{"two keys sent", func() *http.Request {
			r := idemRequest("POST", "/svc/v2/orders", idemKey1)
			r.Header.Add("Idempotency-Key", idemKey1)
			return r
		}(), true, `"codes":[180]`, 0},
		{"no tenant", idemRequest("POST", "/svc/v2/orders", ""), false, "", 1},
		{"a GET", idemRequest("GET", "/svc/v2/orders", ""), true, "", 1},
		{"an API not listed", idemRequest("POST", "/svc/v2/other", ""), true, "", 1},
		{"enforced without a key", idemRequest("PUT", "/svc/v2/orders/7", ""), true, `"codes":[182]`, 0},
	}
	for _, c := range cases {
		before := ie.calls.Load()
		rec := ie.serve(c.req, c.tenant)
		if c.wantCode != "" && !strings.Contains(rec.Body.String(), c.wantCode) {
			t.Errorf("%s: %d %s", c.name, rec.Code, rec.Body.String())
		}
		if got := ie.calls.Load() - before; got != c.wantCalls {
			t.Errorf("%s: %d calls to the service", c.name, got)
		}
	}
}

func TestIdempotency_EnforceAndModel(t *testing.T) {
	// the global enforce applies when the service has none; an unknown model turns it off
	ie := newIdemEnv(t, map[string]interface{}{
		"model": "memory", "enforce": true,
		"services": map[string]interface{}{"svc": map[string]interface{}{"enabled": true}},
	})
	if rec := ie.serve(idemRequest("DELETE", "/svc/v2/x", ""), true); rec.Code != http.StatusPreconditionRequired {
		t.Errorf("global enforce: %d", rec.Code)
	}
	ie.e.reg.data.CustomRegistry["gateway"].(map[string]interface{})["value"].(map[string]interface{})["idempotency"].(map[string]interface{})["model"] = "redis"
	if rec := ie.serve(idemRequest("POST", "/svc/v2/x", idemKey1), true); rec.Code != http.StatusCreated {
		t.Errorf("unknown model: %d", rec.Code)
	}
	if ie.model.Get(idempotencyKey{"t1", idemKey1}) != nil {
		t.Error("an unknown model writes nothing")
	}
}

func TestIdempotency_ReplayAndRelease(t *testing.T) {
	ie := newIdemEnv(t, map[string]interface{}{
		"model":    "memory",
		"services": map[string]interface{}{"svc": map[string]interface{}{"enabled": true, "ttl": 60000.0}},
	})
	first := ie.serve(idemRequest("POST", "/svc/v2/x", idemKey1), true)
	if first.Code != http.StatusCreated || ie.calls.Load() != 1 {
		t.Fatalf("first: %d", first.Code)
	}
	// the same key replays the recorded answer without calling the service
	again := ie.serve(idemRequest("POST", "/svc/v2/x?other=1", idemKey1), true)
	if again.Code != http.StatusCreated || ie.calls.Load() != 1 || again.Body.String() != "answer�" ||
		again.Header().Get("X-Call") != "x" || len(again.Header().Values("Set-Cookie")) != 2 {
		t.Errorf("replay: %d %q %v", again.Code, again.Body.String(), again.Header())
	}
	// a key in flight answers 409
	ie.model.Lock(idempotencyKey{"t1", idemKey2}, time.Minute, "other")
	if rec := ie.serve(idemRequest("POST", "/svc/v2/x", idemKey2), true); !strings.Contains(rec.Body.String(), `"codes":[181]`) || rec.Code != http.StatusConflict {
		t.Errorf("in flight: %d %s", rec.Code, rec.Body.String())
	}
	ie.model.Unlock(idempotencyKey{"t1", idemKey2}, "other")

	// a 5xx releases the key: the retry reaches the service
	ie.status.Store(http.StatusServiceUnavailable)
	ie.serve(idemRequest("POST", "/svc/v2/x", idemKey2), true)
	ie.status.Store(http.StatusOK)
	if rec := ie.serve(idemRequest("POST", "/svc/v2/x", idemKey2), true); rec.Code != http.StatusOK || ie.calls.Load() != 3 {
		t.Errorf("after a 5xx: %d, %d calls", rec.Code, ie.calls.Load())
	}

	// a request answered before the service (no healthy host, 133) releases the key
	k := idempotencyKey{"t1", "1b2d4f6a-1c3e-4a5b-8d7f-0e1f2a3b4c5d"}
	delete(ie.e.aw.hosts, "svc:2")
	if rec := ie.serve(idemRequest("POST", "/svc/v2/x", k.l2), true); !strings.Contains(rec.Body.String(), `"codes":[133]`) {
		t.Fatalf("no host: %s", rec.Body.String())
	}
	if ie.model.Get(k) != nil {
		t.Error("the key stays locked after an answer of the gateway")
	}
}

func TestMemoryIdempotency(t *testing.T) {
	now := time.Unix(1000, 0)
	m := &MemoryIdempotency{tenants: map[string]map[string]*idempotencyEntry{}, now: func() time.Time { return now }}
	k := idempotencyKey{"t", idemKey1}
	if !m.Lock(k, time.Second, "a") || m.Lock(k, time.Second, "b") {
		t.Fatal("a held key cannot be locked again")
	}
	m.Unlock(k, "b")
	if e := m.Get(k); e == nil || e.Status != idempotencyInFlight {
		t.Fatal("another lockId does not release it")
	}
	m.Complete(k, &RecordedResponse{StatusCode: 200}, time.Second)
	m.Unlock(k, "a")
	if e := m.Get(k); e == nil || e.Status != idempotencyCompleted {
		t.Fatal("a completed key is never released")
	}
	now = now.Add(2 * time.Second)
	if m.Get(k) != nil || !m.Lock(k, time.Second, "c") {
		t.Fatal("an expired key is free")
	}
	now = now.Add(2 * time.Second)
	m.Cleanup()
	if len(m.tenants) != 0 {
		t.Errorf("cleanup left %v", m.tenants)
	}
}

// The mongo model on a local MongoDB (MONGO_URI, default mongodb://127.0.0.1:27017), in a
// conformance_ database it drops; skipped without one.
func TestMongoIdempotency(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := drv.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(2*time.Second))
	if err == nil {
		err = client.Ping(ctx, nil)
	}
	if err != nil {
		t.Skip("no local MongoDB: " + err.Error())
	}
	defer client.Disconnect(context.Background())
	db := client.Database("conformance_idempotency_test")
	_ = db.Drop(ctx)
	defer db.Drop(context.Background())

	m := NewMongoIdempotency(db, zap.NewNop())
	k := idempotencyKey{"t1", idemKey2}
	if !m.Lock(k, time.Minute, "a") || m.Lock(k, time.Minute, "b") {
		t.Fatal("a held key cannot be locked again")
	}
	if e := m.Get(k); e == nil || e.Status != idempotencyInFlight || e.LockID != "a" {
		t.Fatalf("in flight: %+v", e)
	}
	m.Unlock(k, "b")
	m.Complete(k, &RecordedResponse{StatusCode: 201, Headers: map[string]interface{}{"set-cookie": []interface{}{"a=1"}, "x": "y"}, Body: "ok"}, time.Minute)
	m.Unlock(k, "a")
	e := m.Get(k)
	if e == nil || e.Status != idempotencyCompleted || e.Response == nil || e.Response.StatusCode != 201 || e.Response.Body != "ok" {
		t.Fatalf("completed: %+v", e)
	}
	rec := httptest.NewRecorder()
	replayRecorded(rec, e.Response)
	if rec.Header().Get("Set-Cookie") != "a=1" || rec.Header().Get("X") != "y" {
		t.Errorf("replayed headers %v", rec.Header())
	}

	// an expired document is free again
	if _, err := db.Collection(idempotencyStore).UpdateOne(ctx, bson.M{"l1": "t1"}, bson.M{"$set": bson.M{"expiresAt": time.Now().Add(-time.Second)}}); err != nil {
		t.Fatal(err)
	}
	if m.Get(k) != nil || !m.Lock(k, time.Minute, "c") {
		t.Error("an expired key is free")
	}
	m.Unlock(k, "c")
	if n, _ := db.Collection(idempotencyStore).CountDocuments(ctx, bson.M{}); n != 0 {
		t.Errorf("unlock left %d documents", n)
	}

	// what is written matches the data schema
	m.Lock(k, time.Minute, "d")
	m.Lock(idempotencyKey{"t1", idemKey1}, time.Minute, "e")
	m.Complete(idempotencyKey{"t1", idemKey1}, &RecordedResponse{StatusCode: 200, Headers: map[string]interface{}{}, Body: ""}, time.Minute)
	schemas, err := dataschema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	cur, err := db.Collection(idempotencyStore).Find(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	defer cur.Close(ctx)
	count := 0
	for cur.Next(ctx) {
		count++
		if problems, err := schemas[idempotencyStore].ValidateBSON(cur.Current); err != nil || problems != nil {
			t.Errorf("idempotency_store does not match its schema: %v %v", problems, err)
		}
	}
	if count != 2 {
		t.Errorf("%d documents", count)
	}
}
