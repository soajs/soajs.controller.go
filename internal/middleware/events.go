package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/soajs/soajs.controller.go/internal/events"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// EventMarks are what a request's call event records besides its identity and routing: the MCP
// tool of a /soajs/mcp tools/call, and an answer replayed by idempotency or the cache.
type EventMarks struct {
	MCPTool     string
	Idempotency string
	Cache       string
}

// eventsConfig is the custom registry gateway events, when enabled.
type eventsConfig struct {
	sink               string
	mongoDB, mongoColl string
	queueSize          int
	services           map[string]interface{}
	hasServices        bool
}

func eventsConfigOf(regData *types.RegistryData) *eventsConfig {
	if regData == nil {
		return nil
	}
	gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	ev, _ := value["events"].(map[string]interface{})
	if ev == nil || ev["enabled"] != true {
		return nil
	}
	c := &eventsConfig{queueSize: number(ev["queueSize"])}
	c.sink, _ = ev["sink"].(string)
	mongo, _ := ev["mongo"].(map[string]interface{})
	c.mongoDB, _ = mongo["db"].(string)
	c.mongoColl, _ = mongo["collection"].(string)
	c.services, c.hasServices = ev["services"].(map[string]interface{})
	return c
}

// serviceOn: without services every service emits; with them, only those enabled.
func (c *eventsConfig) serviceOn(service string) bool {
	if !c.hasServices {
		return true
	}
	s, _ := c.services[service].(map[string]interface{})
	return s != nil && s["enabled"] == true
}

// DatabaseFor opens a core database of the environment by its name in coreDB.
type DatabaseFor func(name string) (*drv.Database, error)

// CallEvents emits a call event for each request from the key step on, as configured by the
// custom registry gateway events {enabled, sink, mongo {db, collection}, queueSize, services}:
// the request's identity and routing, its status and SOAJS code, how long it took; never bodies,
// query values, headers, tokens or keys. Events are queued and written by a background worker,
// to a time-series collection (sink mongo) or the log (sink log). Off, it changes nothing.
type CallEvents struct {
	reg    registry.Registry
	dbFor  DatabaseFor
	gw     GatewayService
	env    string
	logger *zap.Logger

	mu       sync.Mutex
	emitter  *events.Emitter
	sinkKey  string
	sink     events.Sink
	reported map[string]bool
	// testSink, when set, replaces the configured sink (tests)
	testSink events.Sink
}

// NewCallEvents creates the call events of the gateway; its queue starts with the first event.
func NewCallEvents(reg registry.Registry, dbFor DatabaseFor, gw GatewayService, env string, logger *zap.Logger) *CallEvents {
	return &CallEvents{reg: reg, dbFor: dbFor, gw: gw, env: strings.ToLower(env), logger: logger, reported: map[string]bool{}}
}

// Emitter is the emitter, nil before the first event.
func (ce *CallEvents) Emitter() *events.Emitter {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	return ce.emitter
}

func (ce *CallEvents) emitterFor(c *eventsConfig) *events.Emitter {
	ce.mu.Lock()
	defer ce.mu.Unlock()
	if ce.emitter == nil {
		ce.emitter = events.NewEmitter(c.queueSize, ce.resolveSink, ce.logger)
	}
	return ce.emitter
}

func (ce *CallEvents) logOnce(id, msg string, fields ...zap.Field) {
	if !ce.reported[id] {
		ce.reported[id] = true
		ce.logger.Error(msg, fields...)
	}
}

// resolveSink is the sink of the current configuration, kept while it does not change.
func (ce *CallEvents) resolveSink() (events.Sink, error) {
	c := eventsConfigOf(ce.reg.Get())
	ce.mu.Lock()
	defer ce.mu.Unlock()
	if c == nil {
		return nil, nil
	}
	if ce.testSink != nil {
		return ce.testSink, nil
	}
	key := c.sink + "|" + c.mongoDB + "|" + c.mongoColl
	if key == ce.sinkKey && ce.sink != nil {
		return ce.sink, nil
	}
	switch c.sink {
	case "log":
		ce.sink, ce.sinkKey = events.LogSink{Logger: ce.logger}, key
	case "mongo":
		if c.mongoDB == "" || c.mongoColl == "" {
			ce.logOnce("mongo-config", "configuration error: call events mongo needs db and collection")
			return nil, nil
		}
		db, err := ce.dbFor(c.mongoDB)
		if err != nil {
			ce.logOnce("mongo-db-"+c.mongoDB, "configuration error: call events mongo db is not a core database of the environment",
				zap.String("db", c.mongoDB), zap.Error(err))
			return nil, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		sink, err := events.NewMongoSink(ctx, db, c.mongoColl)
		if err != nil {
			return nil, err
		}
		ce.sink, ce.sinkKey = sink, key
	default:
		ce.logOnce("sink-"+c.sink, "configuration error: unknown call events sink ["+c.sink+"]; it can only be [mongo || log]")
		return nil, nil
	}
	return ce.sink, nil
}

// Middleware is the call events middleware, before the key step.
func (ce *CallEvents) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c := eventsConfigOf(ce.reg.Get())
			soajsCtx := GetSOAJSContext(r.Context())
			if c == nil || soajsCtx == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil ||
				soajsCtx.Controller.ServiceParams.URLInfo == nil {
				next.ServeHTTP(w, r)
				return
			}
			start := time.Now()
			ew := &eventWriter{ResponseWriter: w}
			next.ServeHTTP(ew, r)
			ev := ce.event(r, soajsCtx, ew, start)
			if ev == nil || !c.serviceOn(ev.Service) {
				return
			}
			ce.emitterFor(c).Emit(ev)
		})
	}
}

// event builds the call event of a request that was answered.
func (ce *CallEvents) event(r *http.Request, soajsCtx *SOAJSContext, ew *eventWriter, start time.Time) *events.Event {
	params := soajsCtx.Controller.ServiceParams
	info := params.URLInfo
	status, code := ew.result()
	ev := &events.Event{
		TS:          start,
		DurationMs:  int(time.Since(start).Milliseconds()),
		RequestID:   chimw.GetReqID(r.Context()),
		Env:         ce.env,
		Service:     info.ServiceName,
		Version:     info.ServiceVersion,
		Method:      r.Method,
		MCPTool:     soajsCtx.Marks.MCPTool,
		Status:      status,
		Code:        code,
		Idempotency: soajsCtx.Marks.Idempotency,
		Cache:       soajsCtx.Marks.Cache,
		IP:          getClientIP(r),
	}
	switch {
	case params.GatewayAPI:
		ev.API = info.ServicePath
	case params.Proxy:
		ev.Service, ev.Version, ev.API = ce.gw.Name, ce.gw.Version, "/soajs/proxy"
	default:
		if api := params.ACL.API(info.ServicePath); api != nil {
			ev.API = api.Route
		}
	}
	tenant := soajsCtx.Tenant
	if tenant == nil && soajsCtx.Key != nil {
		tenant = types.TenantFromKey(soajsCtx.Key)
	}
	if tenant != nil && tenant.ID != "" {
		ev.Tenant = &events.Tenant{ID: tenant.ID, Code: tenant.Code}
		if tenant.Main != nil && tenant.Main.ID != "" {
			ev.Tenant.Main = &events.TenantRef{ID: tenant.Main.ID, Code: tenant.Main.Code}
		}
	}
	if key := soajsCtx.Key; key != nil {
		ev.Product, ev.Package, ev.AppID = key.Application.Product, key.Application.Package, key.Application.AppID
	}
	ev.User = eventUser(soajsCtx, params)
	return ev
}

// eventUser is the signed-in user: from the user record when it was read, else from the token.
func eventUser(soajsCtx *SOAJSContext, params *types.ServiceParams) *events.User {
	var tokenUser map[string]interface{}
	if params.OAuthToken != nil {
		tokenUser = params.OAuthToken.User
	}
	u := &events.User{}
	if user := soajsCtx.Urac; user != nil {
		u.ID = user.ID()
		if tu := user.TokenUser(); tu != nil {
			tokenUser = tu
		}
		if profile := user.Profile(false); profile != nil {
			u.Username, _ = profile["username"].(string)
		}
	}
	if tokenUser != nil {
		if u.ID == "" {
			u.ID, _ = tokenUser["id"].(string)
		}
		if u.Username == "" {
			u.Username, _ = tokenUser["username"].(string)
		}
		restrictedTo, _ := tokenUser["restrictedTo"].(map[string]interface{})
		u.Agent = len(restrictedTo) > 0
	}
	if u.ID == "" && u.Username == "" && !u.Agent {
		return nil
	}
	return u
}

// eventWriter records the status and the SOAJS code of an answer.
type eventWriter struct {
	http.ResponseWriter
	mu     sync.Mutex
	status int
	code   int
}

func (ew *eventWriter) WriteHeader(status int) {
	ew.mu.Lock()
	if ew.status == 0 {
		ew.status = status
	}
	ew.mu.Unlock()
	ew.ResponseWriter.WriteHeader(status)
}

func (ew *eventWriter) Write(b []byte) (int, error) {
	ew.mu.Lock()
	if ew.status == 0 {
		ew.status = http.StatusOK
	}
	ew.mu.Unlock()
	return ew.ResponseWriter.Write(b)
}

func (ew *eventWriter) Flush() {
	if f, ok := ew.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (ew *eventWriter) Unwrap() http.ResponseWriter { return ew.ResponseWriter }

func (ew *eventWriter) recordCode(code int) {
	ew.mu.Lock()
	if ew.code == 0 {
		ew.code = code
	}
	ew.mu.Unlock()
}

func (ew *eventWriter) result() (int, int) {
	ew.mu.Lock()
	defer ew.mu.Unlock()
	status := ew.status
	if status == 0 {
		status = http.StatusOK
	}
	return status, ew.code
}

// noteCode tells the call events of a request the SOAJS code it is answered with.
func noteCode(w http.ResponseWriter, code int) {
	for w != nil {
		if r, ok := w.(interface{ recordCode(int) }); ok {
			r.recordCode(code)
			return
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return
		}
		w = u.Unwrap()
	}
}
