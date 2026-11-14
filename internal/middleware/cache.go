package middleware

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

const (
	cacheStore        = "cache_store"
	cacheInvalidation = "cache_invalidation"
	// an invalidation is remembered long enough to outlive any request that started before it
	cacheInvalidationRetention = 15 * time.Minute
)

// cacheKey is what is cached: the tenant (l1) and service:GET:api path:query hash[:u:user] (l2).
type cacheKey struct{ l1, l2 string }

// cacheMeta describes an entry for invalidation: the service, the API as configured, the user of
// a tenant_user entry, and when the request started.
type cacheMeta struct {
	service, api string
	user         *string
	startedAt    time.Time
}

// cacheEntry is a cached answer.
type cacheEntry struct {
	Response *RecordedResponse
	CachedAt time.Time
}

// CacheModel keeps the cached answers (mw/cache/model).
type CacheModel interface {
	// Get returns the entry of a key, nil when there is none or it has expired.
	Get(k cacheKey) *cacheEntry
	// Set stores an answer for ttl, unless an invalidation covering it came after the request
	// started.
	Set(k cacheKey, resp *RecordedResponse, ttl time.Duration, meta cacheMeta)
}

// CacheContext is the entry a request will fill with the service's answer.
type CacheContext struct {
	key   cacheKey
	ttl   time.Duration
	meta  cacheMeta
	model CacheModel
}

// save stores a 2xx answer. A nil context does nothing.
func (c *CacheContext) save(resp *RecordedResponse) {
	if c == nil || resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return
	}
	c.model.Set(c.key, resp, c.ttl, c.meta)
}

// Cache answers a key's GET API from the custom registry gateway cache, as the Node.js cache
// middleware does (mw/cache): for a service with enabled and an API of its apis ("GET /path/:param")
// with enabled, the entry of the tenant is looked up in the model (memory by default):
//   - the scope is the API's scope, tenant or tenant_user, else tenant for a public API and
//     tenant_user for a private one; a tenant_user entry needs the token's user, else nothing is
//     cached;
//   - the key is service:GET:api path:md5 of the sorted query without access_token, with :u:user
//     for tenant_user;
//   - a hit answers the recorded answer with X-Cache HIT and X-Cache-Age (seconds); a miss sets
//     X-Cache MISS, and a 2xx answer of the service is stored for the API's ttl, else the
//     defaultTTL, else 300000 ms.
func Cache(reg registry.Registry, models map[string]CacheModel, logger *zap.Logger) Middleware {
	var once sync.Map
	logOnce := func(id, msg string) {
		if _, logged := once.LoadOrStore(id, true); !logged {
			logger.Error(msg)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			regData := reg.Get()
			if r.Method != http.MethodGet || soajsCtx == nil || soajsCtx.Tenant == nil || soajsCtx.Controller == nil ||
				soajsCtx.Controller.ServiceParams == nil || soajsCtx.Controller.ServiceParams.URLInfo == nil || regData == nil {
				next.ServeHTTP(w, r)
				return
			}
			gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
			value, _ := gateway["value"].(map[string]interface{})
			config, _ := value["cache"].(map[string]interface{})
			if config == nil {
				next.ServeHTTP(w, r)
				return
			}
			params := soajsCtx.Controller.ServiceParams
			info := params.URLInfo
			services, _ := config["services"].(map[string]interface{})
			service, _ := services[info.ServiceName].(map[string]interface{})
			if service == nil || !truthy(service["enabled"]) {
				next.ServeHTTP(w, r)
				return
			}
			apiPath := "/"
			if segments := strings.Split(info.OriginalPath, "/"); len(segments) > 2 {
				apiPath = "/" + strings.Join(segments[2:], "/")
			}
			apis, _ := service["apis"].(map[string]interface{})
			apiName, apiConfig := matchCachedAPI("GET", apiPath, apis)
			if apiConfig == nil || !truthy(apiConfig["enabled"]) {
				next.ServeHTTP(w, r)
				return
			}
			modelName := "memory"
			if m, ok := config["model"].(string); ok && m != "" {
				modelName = m
			}
			model := models[modelName]
			if model == nil {
				logOnce("model_"+modelName, "Cache: Unknown model ["+modelName+"]. It can only be [memory || mongo]. Caching is off.")
				next.ServeHTTP(w, r)
				return
			}
			scope, _ := apiConfig["scope"].(string)
			if scope != "tenant" && scope != "tenant_user" {
				if truthy(apiConfig["scope"]) {
					logOnce("scope_"+scope+"_GET_"+apiPath, "Cache: Unknown scope ["+scope+"] for API [GET "+apiPath+"]. It can only be [tenant || tenant_user]. Falling back to default.")
				}
				scope = "tenant_user"
				if params.IsAPIPublic {
					scope = "tenant"
				}
			}
			userID := ""
			if soajsCtx.Urac != nil {
				userID = soajsCtx.Urac.ID()
			}
			if scope == "tenant_user" && userID == "" {
				next.ServeHTTP(w, r)
				return
			}
			ttl := time.Duration(number(apiConfig["ttl"])) * time.Millisecond
			if ttl == 0 {
				ttl = time.Duration(number(config["defaultTTL"])) * time.Millisecond
			}
			if ttl == 0 {
				ttl = 300000 * time.Millisecond
			}
			meta := cacheMeta{service: info.ServiceName, api: apiName, startedAt: time.Now()}
			l2 := info.ServiceName + ":GET:" + apiPath + ":" + hashCacheQuery(parseQueryString(r.URL.RawQuery))
			if scope == "tenant_user" {
				l2 += ":u:" + userID
				meta.user = &userID
			}
			key := cacheKey{l1: soajsCtx.Tenant.ID, l2: l2}
			if cached := model.Get(key); cached != nil && cached.Response != nil {
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("X-Cache-Age", strconv.FormatInt(int64(time.Since(cached.CachedAt)/time.Second), 10))
				replayRecorded(w, cached.Response)
				return
			}
			soajsCtx.Cache = &CacheContext{key: key, ttl: ttl, meta: meta, model: model}
			w.Header().Set("X-Cache", "MISS")
			next.ServeHTTP(w, r)
		})
	}
}

// matchCachedAPI finds the API of a request in a service's cache apis: the "METHOD /path" entry,
// else the first entry, in sorted order, that matches segment by segment, a ":param" segment
// matching any. It returns the entry's name and configuration.
func matchCachedAPI(method, path string, apis map[string]interface{}) (string, map[string]interface{}) {
	signature := method + " " + path
	if c, ok := apis[signature].(map[string]interface{}); ok {
		return signature, c
	}
	names := make([]string, 0, len(apis))
	for name := range apis {
		names = append(names, name)
	}
	sort.Strings(names)
	pathParts := strings.Split(path, "/")
	for _, name := range names {
		parts := strings.Split(name, " ")
		if len(parts) < 2 || parts[0] != method {
			continue
		}
		apiParts := strings.Split(parts[1], "/")
		if len(apiParts) != len(pathParts) {
			continue
		}
		match := true
		for i, p := range apiParts {
			if !strings.HasPrefix(p, ":") && p != pathParts[i] {
				match = false
				break
			}
		}
		if match {
			c, _ := apis[name].(map[string]interface{})
			return name, c
		}
	}
	return "", nil
}

// hashCacheQuery is hashQuery: the md5 of JSON.stringify of the query without access_token, its
// keys sorted as JavaScript sorts strings and laid out as a JavaScript object does (integer keys
// first, ascending); "" without parameters.
func hashCacheQuery(q *queryString) string {
	var keys []string
	for _, k := range q.keys {
		if k != "access_token" {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Slice(keys, func(i, j int) bool { return jsLess(keys[i], keys[j]) })
	var index, named []string
	for _, k := range keys {
		if isArrayIndex(k) {
			index = append(index, k)
		} else {
			named = append(named, k)
		}
	}
	sort.SliceStable(index, func(i, j int) bool {
		a, _ := strconv.ParseUint(index[i], 10, 32)
		b, _ := strconv.ParseUint(index[j], 10, 32)
		return a < b
	})
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range append(index, named...) {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(jsJSONString(k))
		b.WriteByte(':')
		b.WriteString(jsJSONString(q.values[k]))
	}
	b.WriteByte('}')
	sum := md5.Sum([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// jsLess compares strings by UTF-16 code units, as Array.prototype.sort does.
func jsLess(a, b string) bool {
	ua, ub := utf16.Encode([]rune(utf8Body([]byte(a)))), utf16.Encode([]rune(utf8Body([]byte(b))))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}

// isArrayIndex: a canonical decimal integer below 2^32-1, a key JavaScript objects order first.
func isArrayIndex(k string) bool {
	if k == "" || (len(k) > 1 && k[0] == '0') {
		return false
	}
	n, err := strconv.ParseUint(k, 10, 64)
	return err == nil && n < 1<<32-1
}

// jsJSONString is JSON.stringify of a string.
func jsJSONString(s string) string {
	const hexDigits = "0123456789abcdef"
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range utf8Body([]byte(s)) {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u00`)
				b.WriteByte(hexDigits[r>>4])
				b.WriteByte(hexDigits[r&15])
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// MemoryCache keeps the entries in memory (model/memory.js), by tenant. Every
// SOAJS_CACHE_CLEANUP_INTERVAL ms (60000) expired entries are dropped.
type MemoryCache struct {
	mu      sync.Mutex
	tenants map[string]map[string]*memoryCacheEntry
	now     func() time.Time
}

type memoryCacheEntry struct {
	cacheEntry
	expiresAt time.Time
}

// NewMemoryCache starts the memory model; its cleanup stops with ctx.
func NewMemoryCache(ctx context.Context) *MemoryCache {
	m := &MemoryCache{tenants: map[string]map[string]*memoryCacheEntry{}, now: time.Now}
	interval := 60000
	if v, err := strconv.Atoi(os.Getenv("SOAJS_CACHE_CLEANUP_INTERVAL")); err == nil && v > 0 {
		interval = v
	}
	go func() {
		t := time.NewTicker(time.Duration(interval) * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.Cleanup()
			}
		}
	}()
	return m
}

// Cleanup drops the expired entries, and the tenants left without any.
func (m *MemoryCache) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for l1, entries := range m.tenants {
		for l2, e := range entries {
			if now.After(e.expiresAt) {
				delete(entries, l2)
			}
		}
		if len(entries) == 0 {
			delete(m.tenants, l1)
		}
	}
}

func (m *MemoryCache) Get(k cacheKey) *cacheEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tenants[k.l1][k.l2]
	if e == nil {
		return nil
	}
	if m.now().After(e.expiresAt) {
		delete(m.tenants[k.l1], k.l2)
		return nil
	}
	c := e.cacheEntry
	return &c
}

// Set stores the answer; the memory model keeps no invalidations.
func (m *MemoryCache) Set(k cacheKey, resp *RecordedResponse, ttl time.Duration, _ cacheMeta) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.tenants[k.l1]
	if entries == nil {
		entries = map[string]*memoryCacheEntry{}
		m.tenants[k.l1] = entries
	}
	now := m.now()
	entries[k.l2] = &memoryCacheEntry{cacheEntry: cacheEntry{Response: resp, CachedAt: now}, expiresAt: now.Add(ttl)}
}

// MongoCache keeps the entries in cache_store and the invalidations in cache_invalidation
// (model/mongo.js).
type MongoCache struct {
	db     *drv.Database
	logger *zap.Logger
}

// NewMongoCache creates the model and its indexes.
func NewMongoCache(db *drv.Database, logger *zap.Logger) *MongoCache {
	m := &MongoCache{db: db, logger: logger}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	lookup := bson.D{{Key: "l1", Value: 1}, {Key: "service", Value: 1}, {Key: "api", Value: 1}, {Key: "user", Value: 1}}
	indexes := []struct {
		coll, name string
		model      drv.IndexModel
	}{
		{cacheStore, "Cache index", drv.IndexModel{Keys: bson.D{{Key: "l1", Value: 1}, {Key: "l2", Value: 1}}, Options: options.Index().SetUnique(true)}},
		{cacheStore, "Cache TTL index", drv.IndexModel{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
		{cacheStore, "Cache invalidation lookup index", drv.IndexModel{Keys: lookup}},
		{cacheInvalidation, "Cache invalidation index", drv.IndexModel{Keys: lookup, Options: options.Index().SetUnique(true)}},
		{cacheInvalidation, "Cache invalidation TTL index", drv.IndexModel{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}},
	}
	for _, i := range indexes {
		if _, err := db.Collection(i.coll).Indexes().CreateOne(ctx, i.model); err != nil {
			logger.Error(i.name + " creation failed: " + err.Error())
		}
	}
	return m
}

// Get reads the entry; a read error, a document that does not match the data schema or an
// expired one is no entry.
func (m *MongoCache) Get(k cacheKey) *cacheEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := m.db.Collection(cacheStore).FindOne(ctx, bson.M{"l1": k.l1, "l2": k.l2}).Raw()
	if err == drv.ErrNoDocuments {
		return nil
	}
	if err != nil {
		m.logger.Error("Cache get: " + err.Error())
		return nil
	}
	if schemas, err := dataschema.Embedded(); err == nil {
		if problems, err := schemas[cacheStore].ValidateBSON(raw); err != nil || problems != nil {
			m.logger.Error("Cache: a cache_store document does not match the data schema, it is skipped", zap.Any("problems", problems), zap.Error(err))
			return nil
		}
	}
	var doc struct {
		Response  *RecordedResponse `bson:"response"`
		CachedAt  time.Time         `bson:"cachedAt"`
		ExpiresAt time.Time         `bson:"expiresAt"`
	}
	if err := bson.Unmarshal(raw, &doc); err != nil {
		m.logger.Error("Cache get: " + err.Error())
		return nil
	}
	if time.Now().After(doc.ExpiresAt) {
		return nil
	}
	return &cacheEntry{Response: doc.Response, CachedAt: doc.CachedAt}
}

// Set stores the answer (upsert), unless an invalidation of its service, for its API or all, and
// its user or all, was made after the request started.
func (m *MongoCache) Set(k cacheKey, resp *RecordedResponse, ttl time.Duration, meta cacheMeta) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var user interface{}
	if meta.user != nil {
		user = *meta.user
	}
	err := m.db.Collection(cacheInvalidation).FindOne(ctx, bson.M{
		"l1":        k.l1,
		"service":   meta.service,
		"api":       bson.M{"$in": bson.A{meta.api, nil}},
		"user":      bson.M{"$in": bson.A{user, nil}},
		"clearedAt": bson.M{"$gt": meta.startedAt},
	}).Err()
	if err == nil {
		return
	}
	if err != drv.ErrNoDocuments {
		m.logger.Error("Cache set: " + err.Error())
		return
	}
	now := time.Now()
	doc := bson.M{"l1": k.l1, "l2": k.l2, "response": resp, "cachedAt": now, "expiresAt": now.Add(ttl),
		"service": meta.service, "api": meta.api, "user": user}
	if _, err := m.db.Collection(cacheStore).UpdateOne(ctx, bson.M{"l1": k.l1, "l2": k.l2}, bson.M{"$set": doc}, options.Update().SetUpsert(true)); err != nil {
		m.logger.Error("Cache set: " + err.Error())
	}
}

// CacheInvalidation is what /cache/invalidate removes: a tenant's service, and optionally one API
// (as configured) and one user.
type CacheInvalidation struct {
	Tenant, Service string
	API, User       *string
}

// InvalidateMatching records the invalidation, kept 15 minutes, then removes the matching entries
// and returns how many there were.
func (m *MongoCache) InvalidateMatching(c CacheInvalidation) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var api, user interface{}
	if c.API != nil {
		api = *c.API
	}
	if c.User != nil {
		user = *c.User
	}
	marker := bson.M{"l1": c.Tenant, "service": c.Service, "api": api, "user": user}
	now := time.Now()
	set := bson.M{"clearedAt": now, "expiresAt": now.Add(cacheInvalidationRetention)}
	for k, v := range marker {
		set[k] = v
	}
	if _, err := m.db.Collection(cacheInvalidation).UpdateOne(ctx, marker, bson.M{"$set": set}, options.Update().SetUpsert(true)); err != nil {
		return 0, err
	}
	condition := bson.M{"l1": c.Tenant, "service": c.Service}
	if c.API != nil {
		condition["api"] = *c.API
	}
	if c.User != nil {
		condition["user"] = *c.User
	}
	res, err := m.db.Collection(cacheStore).DeleteMany(ctx, condition)
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

var cacheAPIRe = regexp.MustCompile(`^GET /\S*$`)

// InvalidateCache is the /cache/invalidate of the maintenance port (mw/cache/invalidate.js) for a
// parsed JSON body: tenant and service, optionally api ("GET /path", as configured) and user. It
// needs the custom registry gateway cache with the mongo model and its store. It returns the HTTP
// status, the number of entries removed, and the error text when the status is not 200.
func InvalidateCache(regData *types.RegistryData, store *MongoCache, input interface{}, logger *zap.Logger) (int, int64, string) {
	body, ok := input.(map[string]interface{})
	if !ok {
		if _, isList := input.([]interface{}); !isList {
			return http.StatusBadRequest, 0, "A JSON body with tenant and service is required."
		}
	}
	tenant, _ := body["tenant"].(string)
	service, _ := body["service"].(string)
	if tenant == "" || service == "" {
		return http.StatusBadRequest, 0, "tenant and service are required."
	}
	c := CacheInvalidation{Tenant: tenant, Service: service}
	if v, has := body["api"]; has {
		api, _ := v.(string)
		if api == "" || !cacheAPIRe.MatchString(api) {
			return http.StatusBadRequest, 0, `api must be written as in the cache configuration, for example "GET /spaces".`
		}
		c.API = &api
	}
	if v, has := body["user"]; has {
		user, _ := v.(string)
		if user == "" {
			return http.StatusBadRequest, 0, "user must be a user id."
		}
		c.User = &user
	}
	var config map[string]interface{}
	if regData != nil {
		gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
		value, _ := gateway["value"].(map[string]interface{})
		config, _ = value["cache"].(map[string]interface{})
	}
	if config == nil {
		return http.StatusBadRequest, 0, "Caching is not configured."
	}
	if model, _ := config["model"].(string); model != "mongo" {
		if model == "" {
			model = "memory"
		}
		return http.StatusBadRequest, 0, "Cache invalidation needs the mongo model, the cache model is [" + model + "]."
	}
	if store == nil {
		return http.StatusInternalServerError, 0, "The cache store is not ready."
	}
	removed, err := store.InvalidateMatching(c)
	if err != nil {
		logger.Error("Cache invalidate: " + err.Error())
		return http.StatusInternalServerError, 0, "Cache invalidation failed."
	}
	return http.StatusOK, removed, ""
}
