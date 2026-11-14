package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/internal/urac"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// The query hash is the one Node.js computes (vectors from Node.js 24), so both gateways share
// mongo entries.
func TestHashCacheQuery(t *testing.T) {
	cases := map[string]string{
		"b=2&a=x+y&10=t&9=u&%C3%A9=%22%0A%01&access_token=z": "5ce82d519b8bfe8036ad5373e7fc157e",
		"access_token=z": "",
		"":               "",
		"z=1&Z=2&%F0%9F%98%80=e&%EF%BC%A1=f&01=g&4294967295=h&4294967294=i": "6f0aa81d7cc03036cbf2aecd58fdb9c7",
		"a=1&a=2": "4c4de1409294f3736c68d84e91377fee",
	}
	for q, want := range cases {
		if got := hashCacheQuery(parseQueryString(q)); got != want {
			t.Errorf("%q: %s, want %s", q, got, want)
		}
	}
}

func TestMatchCachedAPI(t *testing.T) {
	apis := map[string]interface{}{
		"GET /items/:id":  map[string]interface{}{"enabled": true},
		"GET /:kind/:id":  map[string]interface{}{"enabled": true},
		"GET /items/list": map[string]interface{}{"enabled": false},
		"POST /items":     map[string]interface{}{"enabled": true},
	}
	cases := map[string]string{"/items/list": "GET /items/list", "/items/7": "GET /:kind/:id", "/other/7": "GET /:kind/:id", "/items": ""}
	for path, want := range cases {
		if got, _ := matchCachedAPI("GET", path, apis); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
}

// cacheEnv is a gateway with the cache middleware on a service redirect.
type cacheEnv struct {
	e     *proxyEnv
	model *MemoryCache
	calls atomic.Int32
	code  atomic.Int32
}

func newCacheEnv(t *testing.T, config map[string]interface{}) *cacheEnv {
	ce := &cacheEnv{}
	ce.code.Store(http.StatusOK)
	ce.e = newProxyEnv(t, func(w http.ResponseWriter, r *http.Request) {
		ce.calls.Add(1)
		w.Header().Set("X-Service", r.URL.RawQuery)
		w.WriteHeader(int(ce.code.Load()))
		_, _ = w.Write([]byte("answer " + r.URL.RawQuery))
	})
	ce.e.reg.data.CustomRegistry = map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"cache": config}}}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ce.model = NewMemoryCache(ctx)
	return ce
}

// serve runs a request as the given user (none when empty), its API public or not.
func (ce *cacheEnv) serve(path, user string, public bool) *httptest.ResponseRecorder {
	e := ce.e
	handler := Context(zap.NewNop(), "test")(URL(nil)(GotoService(e.reg, e.aw, e.monitor(), &mockProvision{}, GatewayService{Name: "controller", Version: "1"})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := GetSOAJSContext(r.Context())
			ctx.Tenant = &types.Tenant{ID: "t1"}
			ctx.Controller.ServiceParams.IsAPIPublic = public
			if user != "" {
				ctx.Urac = testUracUser(user)
			}
			Cache(e.reg, map[string]CacheModel{"memory": ce.model}, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				GetSOAJSContext(r.Context()).Controller.Gotoservice(w, r)
			})).ServeHTTP(w, r)
		}))))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestCache_HitMissScope(t *testing.T) {
	ce := newCacheEnv(t, map[string]interface{}{
		"model": "memory",
		"services": map[string]interface{}{"svc": map[string]interface{}{"enabled": true, "apis": map[string]interface{}{
			"GET /pub":       map[string]interface{}{"enabled": true},
			"GET /priv/:id":  map[string]interface{}{"enabled": true, "ttl": 60000.0},
			"GET /shared":    map[string]interface{}{"enabled": true, "scope": "tenant"},
			"GET /off":       map[string]interface{}{"enabled": false},
			"GET /bad-scope": map[string]interface{}{"enabled": true, "scope": "everyone"},
		}}},
	})
	type step struct {
		path, user string
		public     bool
		xCache     string
		calls      int32
	}
	steps := []step{
		// public: per tenant, the token in the query is not part of the key
		{"/svc/v2/pub?a=1", "", true, "MISS", 1},
		{"/svc/v2/pub?access_token=x&a=1", "u2", true, "HIT", 1},
		{"/svc/v2/pub?a=2", "", true, "MISS", 2},
		// private: per user; without a user nothing is cached
		{"/svc/v2/priv/7", "u1", false, "MISS", 3},
		{"/svc/v2/priv/7", "u1", false, "HIT", 3},
		{"/svc/v2/priv/7", "u2", false, "MISS", 4},
		{"/svc/v2/priv/7", "", false, "", 5},
		// scope tenant on a private API is shared by users
		{"/svc/v2/shared", "u1", false, "MISS", 6},
		{"/svc/v2/shared", "u2", false, "HIT", 6},
		// disabled; an unknown scope falls back to the default
		{"/svc/v2/off", "u1", false, "", 7},
		{"/svc/v2/bad-scope", "", true, "MISS", 8},
		{"/svc/v2/bad-scope", "", true, "HIT", 8},
	}
	for i, s := range steps {
		rec := ce.serve(s.path, s.user, s.public)
		if rec.Header().Get("X-Cache") != s.xCache || ce.calls.Load() != s.calls {
			t.Errorf("step %d %s: X-Cache %q, %d calls", i, s.path, rec.Header().Get("X-Cache"), ce.calls.Load())
		}
	}
	hit := ce.serve("/svc/v2/pub?a=1", "", true)
	if hit.Body.String() != "answer a=1" || hit.Header().Get("X-Service") != "a=1" || hit.Header().Get("X-Cache-Age") != "0" {
		t.Errorf("hit: %q %v", hit.Body.String(), hit.Header())
	}
	// only 2xx answers are stored
	ce.code.Store(http.StatusNotFound)
	ce.serve("/svc/v2/pub?a=404", "", true)
	ce.code.Store(http.StatusOK)
	if rec := ce.serve("/svc/v2/pub?a=404", "", true); rec.Header().Get("X-Cache") != "MISS" {
		t.Errorf("a 404 was stored")
	}
}

func TestMemoryCache(t *testing.T) {
	now := time.Unix(1000, 0)
	m := &MemoryCache{tenants: map[string]map[string]*memoryCacheEntry{}, now: func() time.Time { return now }}
	k := cacheKey{"t", "svc:GET:/x:"}
	m.Set(k, &RecordedResponse{StatusCode: 200}, time.Second, cacheMeta{})
	if e := m.Get(k); e == nil || !e.CachedAt.Equal(now) {
		t.Fatal("stored")
	}
	now = now.Add(2 * time.Second)
	if m.Get(k) != nil {
		t.Fatal("expired")
	}
	m.Set(k, &RecordedResponse{StatusCode: 200}, time.Second, cacheMeta{})
	now = now.Add(2 * time.Second)
	m.Cleanup()
	if len(m.tenants) != 0 {
		t.Errorf("cleanup left %v", m.tenants)
	}
}

func TestInvalidateCache_Input(t *testing.T) {
	mongoReg := &types.RegistryData{CustomRegistry: map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"cache": map[string]interface{}{"model": "mongo"}}}}}
	memoryReg := &types.RegistryData{CustomRegistry: map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"cache": map[string]interface{}{"services": map[string]interface{}{}}}}}}
	parse := func(s string) interface{} {
		var v interface{}
		_ = json.Unmarshal([]byte(s), &v)
		return v
	}
	cases := []struct {
		reg    *types.RegistryData
		body   string
		status int
		text   string
	}{
		{mongoReg, ``, 400, "A JSON body"},
		{mongoReg, `"x"`, 400, "A JSON body"},
		{mongoReg, `[]`, 400, "tenant and service are required."},
		{mongoReg, `{"tenant":"t"}`, 400, "tenant and service are required."},
		{mongoReg, `{"tenant":"t","service":"s","api":null}`, 400, "api must be"},
		{mongoReg, `{"tenant":"t","service":"s","api":"GET spaces"}`, 400, "api must be"},
		{mongoReg, `{"tenant":"t","service":"s","user":""}`, 400, "user must be"},
		{&types.RegistryData{}, `{"tenant":"t","service":"s"}`, 400, "Caching is not configured."},
		{memoryReg, `{"tenant":"t","service":"s"}`, 400, "the cache model is [memory]."},
		{mongoReg, `{"tenant":"t","service":"s","api":"GET /x","user":"u"}`, 500, "The cache store is not ready."},
	}
	for _, c := range cases {
		status, _, text := InvalidateCache(c.reg, nil, parse(c.body), zap.NewNop())
		if status != c.status || !strings.Contains(text, c.text) {
			t.Errorf("%s: %d %q", c.body, status, text)
		}
	}
}

// The mongo model on a local MongoDB (MONGO_URI, default mongodb://127.0.0.1:27017), in a
// conformance_ database it drops; skipped without one.
func TestMongoCache(t *testing.T) {
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
	db := client.Database("conformance_cache_test")
	_ = db.Drop(ctx)
	defer db.Drop(context.Background())

	m := NewMongoCache(db, zap.NewNop())
	u1 := "u1"
	resp := &RecordedResponse{StatusCode: 200, Headers: map[string]interface{}{"x": "y"}, Body: "ok"}
	k1 := cacheKey{"t1", "svc:GET:/a::u:u1"}
	k2 := cacheKey{"t1", "svc:GET:/b:"}
	m.Set(k1, resp, time.Minute, cacheMeta{service: "svc", api: "GET /a", user: &u1, startedAt: time.Now()})
	m.Set(k2, resp, time.Minute, cacheMeta{service: "svc", api: "GET /b", startedAt: time.Now()})
	if e := m.Get(k1); e == nil || e.Response.Body != "ok" || e.Response.Headers["x"] != "y" {
		t.Fatalf("stored: %+v", e)
	}

	// an invalidation of one api removes only its entries, and blocks a request that started before it
	api := "GET /a"
	started := time.Now()
	n, err := m.InvalidateMatching(CacheInvalidation{Tenant: "t1", Service: "svc", API: &api})
	if err != nil || n != 1 || m.Get(k1) != nil || m.Get(k2) == nil {
		t.Fatalf("invalidate api: %d %v", n, err)
	}
	m.Set(k1, resp, time.Minute, cacheMeta{service: "svc", api: "GET /a", user: &u1, startedAt: started.Add(-time.Second)})
	if m.Get(k1) != nil {
		t.Error("a request that started before the invalidation is not stored")
	}
	m.Set(k1, resp, time.Minute, cacheMeta{service: "svc", api: "GET /a", user: &u1, startedAt: time.Now().Add(time.Second)})
	if m.Get(k1) == nil {
		t.Error("a request after the invalidation is stored")
	}
	// the whole service
	if n, err := m.InvalidateMatching(CacheInvalidation{Tenant: "t1", Service: "svc"}); err != nil || n != 2 {
		t.Errorf("invalidate service: %d %v", n, err)
	}

	// what is written matches the data schema
	m.Set(k2, resp, time.Minute, cacheMeta{service: "svc", api: "GET /b", startedAt: time.Now().Add(time.Second)})
	schemas, err := dataschema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, coll := range []string{cacheStore, cacheInvalidation} {
		cur, err := db.Collection(coll).Find(ctx, bson.M{})
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for cur.Next(ctx) {
			count++
			if problems, err := schemas[coll].ValidateBSON(cur.Current); err != nil || problems != nil {
				t.Errorf("%s does not match its schema: %v %v", coll, problems, err)
			}
		}
		cur.Close(ctx)
		if count == 0 {
			t.Errorf("no %s documents", coll)
		}
	}

	// /cache/invalidate with the store
	reg := &types.RegistryData{CustomRegistry: map[string]interface{}{"gateway": map[string]interface{}{"value": map[string]interface{}{"cache": map[string]interface{}{"model": "mongo"}}}}}
	if status, removed, _ := InvalidateCache(reg, m, map[string]interface{}{"tenant": "t1", "service": "svc"}, zap.NewNop()); status != 200 || removed != 1 {
		t.Errorf("invalidate: %d %d", status, removed)
	}
}

// testUracUser is a user whose token has the id.
func testUracUser(id string) *urac.User {
	return urac.NewUser(nil, &urac.Request{Token: &types.AccessToken{User: map[string]interface{}{"id": id}}})
}
