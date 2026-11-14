package middleware

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// throttlingWindow is one counter of requests: when it started and how many it counted.
type throttlingWindow struct {
	FirstReqTime time.Time
	Count        int
}

// trafficKey is what is throttled: the tenant (l1) and the client IP (l2).
type trafficKey struct{ l1, l2 string }

// throttlingStrategy is a strategy of services.config.throttling.
type throttlingStrategy struct {
	perIP   bool // type 1: per tenant and IP; otherwise per tenant
	window  int
	limit   int
	retries int
	delay   int
}

// ThrottlingModel keeps the counters (mw/traffic/model).
type ThrottlingModel interface {
	Get(s throttlingStrategy, k trafficKey) throttlingWindow
	Reset(s throttlingStrategy, k trafficKey) throttlingWindow
	Increment(s throttlingStrategy, k trafficKey)
	LogTooManyRequests(k trafficKey, strategy string)
}

// Traffic throttles a key's API as the Node.js traffic middleware does (mw/traffic): the API's
// strategy (publicAPIStrategy or privateAPIStrategy of services.config.throttling, overridden by
// the services configuration's gateway.throttling, else the custom registry gateway
// traffic.throttling, for the service, or only for its listed apis), counted by tenant and, for a
// strategy of type 1, client IP, in the custom registry traffic.model (memory by default). Over the
// limit, the request is checked again after delay, up to retries times, then answered 429 with
// Retry-After, X-RateLimit-Limit and X-RateLimit-Remaining.
func Traffic(reg registry.Registry, models map[string]ThrottlingModel, logger *zap.Logger) Middleware {
	var once sync.Map
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			soajsCtx := GetSOAJSContext(r.Context())
			regData := reg.Get()
			if soajsCtx == nil || soajsCtx.Tenant == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.URLInfo == nil || regData == nil || regData.ServiceConfig == nil || regData.ServiceConfig.Throttling == nil {
				next.ServeHTTP(w, r)
				return
			}
			params := soajsCtx.Controller.ServiceParams
			throttlingConfig := regData.ServiceConfig.Throttling
			strategyKey := "privateAPIStrategy"
			if params.IsAPIPublic {
				strategyKey = "publicAPIStrategy"
			}
			strategyName := throttlingConfig[strategyKey]

			gateway, _ := soajsCtx.ServicesConfig["gateway"].(map[string]interface{})
			override, _ := gateway["throttling"].(map[string]interface{})
			if override == nil {
				custom, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
				value, _ := custom["value"].(map[string]interface{})
				traffic, _ := value["traffic"].(map[string]interface{})
				override, _ = traffic["throttling"].(map[string]interface{})
			}
			if service, ok := override[params.URLInfo.ServiceName].(map[string]interface{}); ok {
				if v, has := service[strategyKey]; has {
					if apis, ok := service["apis"].([]interface{}); ok {
						for _, a := range apis {
							if a == params.URLInfo.ServicePath {
								strategyName = v
							}
						}
					} else {
						strategyName = v
					}
				}
			}
			name, _ := strategyName.(string)
			if !truthy(strategyName) || name == "" {
				next.ServeHTTP(w, r)
				return
			}
			config, ok := throttlingConfig[name].(map[string]interface{})
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			modelName := "memory"
			custom, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
			value, _ := custom["value"].(map[string]interface{})
			traffic, _ := value["traffic"].(map[string]interface{})
			if m, ok := traffic["model"]; ok && truthy(m) {
				s, _ := m.(string)
				modelName = s
			}
			model := models[modelName]
			if model == nil {
				if _, logged := once.LoadOrStore(modelName, true); !logged {
					logger.Error("Throttling: Unknown model [" + modelName + "]. It can only be [memory || mongo]. Throttling is off.")
				}
				next.ServeHTTP(w, r)
				return
			}
			strategy := throttlingStrategy{
				perIP:   number(config["type"]) == 1,
				window:  number(config["window"]),
				limit:   number(config["limit"]),
				retries: number(config["retries"]),
				delay:   number(config["delay"]),
			}
			key := trafficKey{l1: soajsCtx.Tenant.ID, l2: getClientIP(r)}
			if headers, ok := checkThrottling(r.Context(), model, strategy, name, key); !ok {
				_ = WriteStatusMsg(w, http.StatusTooManyRequests, "too many requests", headers, false)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func number(v interface{}) int {
	f, _ := v.(float64)
	return int(f)
}

// checkThrottling counts the request and, over the limit, checks again after delay up to retries
// times. It returns false with the 429 headers when the request is refused.
func checkThrottling(ctx context.Context, model ThrottlingModel, s throttlingStrategy, name string, k trafficKey) (map[string]string, bool) {
	for retry := 0; ; {
		window := model.Get(s, k)
		remaining := s.window - int(time.Since(window.FirstReqTime).Milliseconds())
		if remaining < 1 {
			window = model.Reset(s, k)
		}
		count := window.Count
		if retry == 0 {
			count++
			model.Increment(s, k)
		}
		if count <= s.limit {
			return nil, true
		}
		retry++
		if retry > s.retries {
			model.LogTooManyRequests(k, name)
			return map[string]string{
				"Retry-After":           strconv.FormatFloat(float64(remaining)/1000, 'f', -1, 64),
				"X-RateLimit-Limit":     strconv.Itoa(s.limit),
				"X-RateLimit-Remaining": strconv.Itoa(s.limit - count),
			}, false
		}
		select {
		case <-ctx.Done():
			return nil, true
		case <-time.After(time.Duration(s.delay) * time.Millisecond):
		}
	}
}

// MemoryThrottling keeps the counters in memory (model/memory.js): by tenant, and under it by IP.
// Every SOAJS_THROTTLE_CLEANUP_INTERVAL ms (60000) counters older than an hour are dropped, a
// tenant's with its IPs'.
type MemoryThrottling struct {
	mu      sync.Mutex
	tenants map[string]*memoryTenant
	logger  *zap.Logger
	now     func() time.Time
}

type memoryTenant struct {
	throttlingWindow
	ips map[string]*throttlingWindow
}

// NewMemoryThrottling returns the memory model and starts its cleanup until ctx is done.
func NewMemoryThrottling(ctx context.Context, logger *zap.Logger) *MemoryThrottling {
	m := &MemoryThrottling{tenants: map[string]*memoryTenant{}, logger: logger, now: time.Now}
	interval := 60000
	if v, err := strconv.Atoi(os.Getenv("SOAJS_THROTTLE_CLEANUP_INTERVAL")); err == nil && v > 0 {
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

func (m *MemoryThrottling) tenant(l1 string) *memoryTenant {
	t := m.tenants[l1]
	if t == nil {
		t = &memoryTenant{throttlingWindow: throttlingWindow{FirstReqTime: m.now()}, ips: map[string]*throttlingWindow{}}
		m.tenants[l1] = t
	}
	return t
}

func (m *MemoryThrottling) window(s throttlingStrategy, k trafficKey) *throttlingWindow {
	t := m.tenant(k.l1)
	if !s.perIP {
		return &t.throttlingWindow
	}
	w := t.ips[k.l2]
	if w == nil {
		w = &throttlingWindow{FirstReqTime: m.now()}
		t.ips[k.l2] = w
	}
	return w
}

// Get returns the counter, created when there is none.
func (m *MemoryThrottling) Get(s throttlingStrategy, k trafficKey) throttlingWindow {
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.window(s, k)
}

// Reset starts the counter over.
func (m *MemoryThrottling) Reset(s throttlingStrategy, k trafficKey) throttlingWindow {
	m.mu.Lock()
	defer m.mu.Unlock()
	w := m.window(s, k)
	*w = throttlingWindow{FirstReqTime: m.now()}
	return *w
}

// Increment counts a request.
func (m *MemoryThrottling) Increment(s throttlingStrategy, k trafficKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.window(s, k).Count++
}

// LogTooManyRequests logs a refused request.
func (m *MemoryThrottling) LogTooManyRequests(k trafficKey, strategy string) {
	m.logger.Error("Throttling [" + strategy + "]rejected: using tenant id [" + k.l1 + "] from ip [" + k.l2 + "]")
}

// Cleanup drops the counters older than an hour.
func (m *MemoryThrottling) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for l1, t := range m.tenants {
		if now.Sub(t.FirstReqTime) > time.Hour {
			delete(m.tenants, l1)
			continue
		}
		for ip, w := range t.ips {
			if now.Sub(w.FirstReqTime) > time.Hour {
				delete(t.ips, ip)
			}
		}
	}
}

// MongoThrottling keeps the counters in the gateway database (model/mongo.js): throttling_monitor
// {l1, l2, firstReqTime, count}, l2 "_ALL_" for a strategy by tenant; refused requests in
// throttling_error {staregy, l1, l2, ts}.
type MongoThrottling struct {
	db     *drv.Database
	logger *zap.Logger
}

// NewMongoThrottling returns the mongo model and creates the {l1: 1, l2: 1} index.
func NewMongoThrottling(db *drv.Database, logger *zap.Logger) *MongoThrottling {
	m := &MongoThrottling{db: db, logger: logger}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.Collection("throttling_monitor").Indexes().CreateOne(ctx, drv.IndexModel{Keys: bson.D{{Key: "l1", Value: 1}, {Key: "l2", Value: 1}}}); err != nil {
		logger.Error("Throttling index creation failed: " + err.Error())
	}
	return m
}

func (m *MongoThrottling) condition(s throttlingStrategy, k trafficKey) bson.M {
	l2 := "_ALL_"
	if s.perIP {
		l2 = k.l2
	}
	return bson.M{"l1": k.l1, "l2": l2}
}

// Get reads the counter; without one it is reset, on error a fresh one is used.
func (m *MongoThrottling) Get(s throttlingStrategy, k trafficKey) throttlingWindow {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := m.db.Collection("throttling_monitor").FindOne(ctx, m.condition(s, k)).Raw()
	if err == drv.ErrNoDocuments {
		return m.Reset(s, k)
	}
	if err != nil {
		m.logger.Error("Throttling: " + err.Error())
		return throttlingWindow{FirstReqTime: time.Now()}
	}
	schemas, err := dataschema.Embedded()
	if err == nil {
		if problems, err := schemas["throttling_monitor"].ValidateBSON(raw); err != nil || problems != nil {
			m.logger.Error("Throttling: a throttling_monitor document does not match the data schema, it is reset", zap.Any("problems", problems))
			return m.Reset(s, k)
		}
	}
	var doc struct {
		FirstReqTime time.Time `bson:"firstReqTime"`
		Count        int       `bson:"count"`
	}
	if err := bson.Unmarshal(raw, &doc); err != nil {
		return m.Reset(s, k)
	}
	return throttlingWindow{FirstReqTime: doc.FirstReqTime, Count: doc.Count}
}

// Reset starts the counter over (upsert).
func (m *MongoThrottling) Reset(s throttlingStrategy, k trafficKey) throttlingWindow {
	w := throttlingWindow{FirstReqTime: time.Now()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := m.db.Collection("throttling_monitor").UpdateOne(ctx, m.condition(s, k),
		bson.M{"$set": bson.M{"firstReqTime": w.FirstReqTime, "count": 0}}, options.Update().SetUpsert(true))
	if err != nil {
		m.logger.Error("Throttling: " + err.Error())
	}
	return w
}

// Increment counts a request.
func (m *MongoThrottling) Increment(s throttlingStrategy, k trafficKey) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := m.db.Collection("throttling_monitor").UpdateOne(ctx, m.condition(s, k), bson.M{"$inc": bson.M{"count": 1}}); err != nil {
		m.logger.Error("Throttling: " + err.Error())
	}
}

// LogTooManyRequests records a refused request; "staregy" is the field name Node.js writes.
func (m *MongoThrottling) LogTooManyRequests(k trafficKey, strategy string) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = m.db.Collection("throttling_error").InsertOne(ctx, bson.D{
			{Key: "staregy", Value: strategy}, {Key: "l1", Value: k.l1}, {Key: "l2", Value: k.l2}, {Key: "ts", Value: time.Now()},
		})
	}()
}
