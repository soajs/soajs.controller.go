package middleware

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

const (
	idempotencyInFlight  = "in_flight"
	idempotencyCompleted = "completed"
	idempotencyStore     = "idempotency_store"
)

var uuidV4 = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// idempotencyKey is what is locked: the tenant (l1) and the Idempotency-Key (l2).
type idempotencyKey struct{ l1, l2 string }

// idempotencyEntry is a lock or a recorded answer.
type idempotencyEntry struct {
	Status    string
	LockID    string
	Response  *RecordedResponse
	ExpiresAt time.Time
}

// IdempotencyModel keeps the locks and the recorded answers (mw/idempotency/model).
type IdempotencyModel interface {
	// Get returns the entry of a key, nil when there is none or it has expired.
	Get(k idempotencyKey) *idempotencyEntry
	// Lock puts the key in flight for ttl unless an entry that has not expired holds it.
	Lock(k idempotencyKey, ttl time.Duration, lockID string) bool
	// Complete records the answer for ttl.
	Complete(k idempotencyKey, resp *RecordedResponse, ttl time.Duration)
	// Unlock removes the key while it is still in flight under lockID.
	Unlock(k idempotencyKey, lockID string)
}

// IdempotencyContext is the lock a request holds. Once the request is sent to the service, the
// service's answer completes or releases it (proxied); otherwise it is released when the request
// is answered.
type IdempotencyContext struct {
	key     idempotencyKey
	ttl     time.Duration
	lockID  string
	model   IdempotencyModel
	proxied bool
}

// finish completes the lock with an answer below 500, else releases it. A nil context does
// nothing.
func (c *IdempotencyContext) finish(resp *RecordedResponse) {
	if c == nil {
		return
	}
	if resp != nil && resp.StatusCode < 500 {
		c.model.Complete(c.key, resp, c.ttl)
		return
	}
	c.model.Unlock(c.key, c.lockID)
}

// release releases the lock. A nil context does nothing.
func (c *IdempotencyContext) release() {
	if c == nil {
		return
	}
	c.model.Unlock(c.key, c.lockID)
}

// Idempotency applies the custom registry gateway idempotency to a key's API, as the Node.js
// idempotency middleware does (mw/idempotency):
//   - an Idempotency-Key that is not a UUID v4 answers 400 (180), on any request;
//   - on a key's API that is not a GET, of a service with enabled, among its apis when listed
//     ("METHOD /path/:param"), a request without the header answers 428 (182) when enforce is on
//     (the service's, else the global one), else goes on;
//   - with the header, the key of the tenant is looked up in the model (memory by default): in
//     flight answers 409 (181), a recorded answer is replayed; otherwise the key is locked for the
//     service's ttl (60000 ms), 409 when another request took it.
//
// The answer of the service completes the lock when below 500 and releases it otherwise; a
// request answered before it reaches the service releases it.
func Idempotency(reg registry.Registry, models map[string]IdempotencyModel, logger *zap.Logger) Middleware {
	var once sync.Map
	logOnce := func(id, msg string) {
		if _, logged := once.LoadOrStore(id, true); !logged {
			logger.Error(msg)
		}
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := strings.Join(r.Header.Values("Idempotency-Key"), ", ")
			if header != "" && !uuidV4.MatchString(header) {
				_ = WriteError(w, http.StatusBadRequest, 180, "Invalid Idempotency-Key format. Expected UUID v4.")
				return
			}
			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Tenant == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}
			method := strings.ToUpper(r.Method)
			regData := reg.Get()
			if method == http.MethodGet || regData == nil {
				next.ServeHTTP(w, r)
				return
			}
			gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
			value, _ := gateway["value"].(map[string]interface{})
			config, _ := value["idempotency"].(map[string]interface{})
			if config == nil {
				next.ServeHTTP(w, r)
				return
			}
			info := soajsCtx.Controller.ServiceParams.URLInfo
			services, _ := config["services"].(map[string]interface{})
			service, _ := services[info.ServiceName].(map[string]interface{})
			if service == nil || !truthy(service["enabled"]) {
				next.ServeHTTP(w, r)
				return
			}
			// the API path: the parsed path after the service segment
			apiPath := "/"
			if segments := strings.Split(info.OriginalPath, "/"); len(segments) > 2 {
				apiPath = "/" + strings.Join(segments[2:], "/")
			}
			if apis, has := service["apis"]; has && truthy(apis) {
				list, ok := apis.([]interface{})
				if !ok {
					logOnce("apis_"+info.ServiceName, "Idempotency: the apis of service ["+info.ServiceName+"] are not a list; the service is skipped.")
					next.ServeHTTP(w, r)
					return
				}
				if !matchesIdempotentAPI(method, apiPath, list) {
					next.ServeHTTP(w, r)
					return
				}
			}
			enforce, isBool := service["enforce"].(bool)
			if !isBool {
				enforce = config["enforce"] == true
			}
			if header == "" {
				if enforce {
					_ = WriteError(w, http.StatusPreconditionRequired, 182, "Idempotency-Key header is required for this API.")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			modelName := "memory"
			if m, ok := config["model"].(string); ok && m != "" {
				modelName = m
			}
			model := models[modelName]
			if model == nil {
				logOnce("model_"+modelName, "Idempotency: Unknown model ["+modelName+"]. It can only be [memory || mongo]. Idempotency is off.")
				next.ServeHTTP(w, r)
				return
			}
			ttl := time.Duration(number(service["ttl"])) * time.Millisecond
			if ttl == 0 {
				ttl = 60000 * time.Millisecond
			}
			key := idempotencyKey{l1: soajsCtx.Tenant.ID, l2: header}

			if existing := model.Get(key); existing != nil {
				if existing.Status == idempotencyInFlight {
					_ = WriteError(w, http.StatusConflict, 181, "Request with this Idempotency-Key is still being processed.")
					return
				}
				if existing.Status == idempotencyCompleted && existing.Response != nil {
					soajsCtx.Marks.Idempotency = "replay"
					replayRecorded(w, existing.Response)
					return
				}
			}
			lockID := newUUIDv4()
			if !model.Lock(key, ttl, lockID) {
				_ = WriteError(w, http.StatusConflict, 181, "Request with this Idempotency-Key is still being processed.")
				return
			}
			ctx := &IdempotencyContext{key: key, ttl: ttl, lockID: lockID, model: model}
			soajsCtx.Idempotency = ctx
			next.ServeHTTP(w, r)
			if !ctx.proxied {
				model.Unlock(key, lockID)
			}
		})
	}
}

// matchesIdempotentAPI is matchesApi: "METHOD /path" exactly, or segment by segment, a ":param"
// segment matching any.
func matchesIdempotentAPI(method, path string, apis []interface{}) bool {
	signature := method + " " + path
	pathParts := strings.Split(path, "/")
	for _, a := range apis {
		api, _ := a.(string)
		if api == signature {
			return true
		}
		parts := strings.Split(api, " ")
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
			return true
		}
	}
	return false
}

// newUUIDv4 is a random UUID v4 (crypto.randomUUID).
func newUUIDv4() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// MemoryIdempotency keeps the entries in memory (model/memory.js), by tenant. Every
// SOAJS_IDEMPOTENCY_CLEANUP_INTERVAL ms (60000) expired entries are dropped.
type MemoryIdempotency struct {
	mu      sync.Mutex
	tenants map[string]map[string]*idempotencyEntry
	now     func() time.Time
}

// NewMemoryIdempotency starts the memory model; its cleanup stops with ctx.
func NewMemoryIdempotency(ctx context.Context) *MemoryIdempotency {
	m := &MemoryIdempotency{tenants: map[string]map[string]*idempotencyEntry{}, now: time.Now}
	interval := 60000
	if v, err := strconv.Atoi(os.Getenv("SOAJS_IDEMPOTENCY_CLEANUP_INTERVAL")); err == nil && v > 0 {
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
func (m *MemoryIdempotency) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for l1, entries := range m.tenants {
		for l2, e := range entries {
			if now.After(e.ExpiresAt) {
				delete(entries, l2)
			}
		}
		if len(entries) == 0 {
			delete(m.tenants, l1)
		}
	}
}

func (m *MemoryIdempotency) Get(k idempotencyKey) *idempotencyEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tenants[k.l1][k.l2]
	if e == nil {
		return nil
	}
	if m.now().After(e.ExpiresAt) {
		delete(m.tenants[k.l1], k.l2)
		return nil
	}
	c := *e
	return &c
}

func (m *MemoryIdempotency) Lock(k idempotencyKey, ttl time.Duration, lockID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.tenants[k.l1]
	if entries == nil {
		entries = map[string]*idempotencyEntry{}
		m.tenants[k.l1] = entries
	}
	now := m.now()
	if e := entries[k.l2]; e != nil && !now.After(e.ExpiresAt) {
		return false
	}
	entries[k.l2] = &idempotencyEntry{Status: idempotencyInFlight, LockID: lockID, ExpiresAt: now.Add(ttl)}
	return true
}

func (m *MemoryIdempotency) Complete(k idempotencyKey, resp *RecordedResponse, ttl time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.tenants[k.l1]
	if entries == nil {
		entries = map[string]*idempotencyEntry{}
		m.tenants[k.l1] = entries
	}
	entries[k.l2] = &idempotencyEntry{Status: idempotencyCompleted, Response: resp, ExpiresAt: m.now().Add(ttl)}
}

func (m *MemoryIdempotency) Unlock(k idempotencyKey, lockID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.tenants[k.l1][k.l2]
	if e == nil || e.Status != idempotencyInFlight || (lockID != "" && e.LockID != lockID) {
		return
	}
	delete(m.tenants[k.l1], k.l2)
}

// MongoIdempotency keeps the entries in idempotency_store (model/mongo.js), with a unique index
// on {l1, l2} and a TTL index on expiresAt.
type MongoIdempotency struct {
	db     *drv.Database
	logger *zap.Logger
}

// NewMongoIdempotency creates the model and its indexes.
func NewMongoIdempotency(db *drv.Database, logger *zap.Logger) *MongoIdempotency {
	m := &MongoIdempotency{db: db, logger: logger}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	indexes := db.Collection(idempotencyStore).Indexes()
	if _, err := indexes.CreateOne(ctx, drv.IndexModel{Keys: bson.D{{Key: "l1", Value: 1}, {Key: "l2", Value: 1}}, Options: options.Index().SetUnique(true)}); err != nil {
		logger.Error("Idempotency index creation failed: " + err.Error())
	}
	if _, err := indexes.CreateOne(ctx, drv.IndexModel{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)}); err != nil {
		logger.Error("Idempotency TTL index creation failed: " + err.Error())
	}
	return m
}

func (m *MongoIdempotency) condition(k idempotencyKey) bson.M {
	return bson.M{"l1": k.l1, "l2": k.l2}
}

// Get reads the entry; a read error, a document that does not match the data schema or an
// expired one is no entry.
func (m *MongoIdempotency) Get(k idempotencyKey) *idempotencyEntry {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := m.db.Collection(idempotencyStore).FindOne(ctx, m.condition(k)).Raw()
	if err == drv.ErrNoDocuments {
		return nil
	}
	if err != nil {
		m.logger.Error("Idempotency get: " + err.Error())
		return nil
	}
	if schemas, err := dataschema.Embedded(); err == nil {
		if problems, err := schemas[idempotencyStore].ValidateBSON(raw); err != nil || problems != nil {
			m.logger.Error("Idempotency: an idempotency_store document does not match the data schema, it is skipped", zap.Any("problems", problems), zap.Error(err))
			return nil
		}
	}
	var doc struct {
		Status    string            `bson:"status"`
		LockID    *string           `bson:"lockId"`
		Response  *RecordedResponse `bson:"response"`
		ExpiresAt time.Time         `bson:"expiresAt"`
	}
	if err := bson.Unmarshal(raw, &doc); err != nil {
		m.logger.Error("Idempotency get: " + err.Error())
		return nil
	}
	if time.Now().After(doc.ExpiresAt) {
		return nil
	}
	e := &idempotencyEntry{Status: doc.Status, Response: doc.Response, ExpiresAt: doc.ExpiresAt}
	if doc.LockID != nil {
		e.LockID = *doc.LockID
	}
	return e
}

// Lock writes the lock in one operation: the upsert matches only a document that has expired (or
// has no expiresAt), so a key that is held fails on the unique index.
func (m *MongoIdempotency) Lock(k idempotencyKey, ttl time.Duration, lockID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now()
	filter := bson.M{"l1": k.l1, "l2": k.l2, "$or": bson.A{bson.M{"expiresAt": bson.M{"$lte": now}}, bson.M{"expiresAt": bson.M{"$exists": false}}}}
	doc := bson.M{"l1": k.l1, "l2": k.l2, "status": idempotencyInFlight, "lockId": lockID, "lockedAt": now, "expiresAt": now.Add(ttl)}
	_, err := m.db.Collection(idempotencyStore).UpdateOne(ctx, filter, bson.M{"$set": doc, "$unset": bson.M{"response": "", "completedAt": ""}}, options.Update().SetUpsert(true))
	if err != nil {
		if !drv.IsDuplicateKeyError(err) {
			m.logger.Error("Idempotency lock: " + err.Error())
		}
		return false
	}
	return true
}

func (m *MongoIdempotency) Complete(k idempotencyKey, resp *RecordedResponse, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	now := time.Now()
	set := bson.M{"status": idempotencyCompleted, "response": resp, "completedAt": now, "expiresAt": now.Add(ttl)}
	if _, err := m.db.Collection(idempotencyStore).UpdateOne(ctx, m.condition(k), bson.M{"$set": set}); err != nil {
		m.logger.Error("Idempotency complete: " + err.Error())
	}
}

func (m *MongoIdempotency) Unlock(k idempotencyKey, lockID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	condition := bson.M{"l1": k.l1, "l2": k.l2, "status": idempotencyInFlight}
	if lockID != "" {
		condition["lockId"] = lockID
	}
	if _, err := m.db.Collection(idempotencyStore).DeleteOne(ctx, condition); err != nil {
		m.logger.Error("Idempotency unlock: " + err.Error())
	}
}
