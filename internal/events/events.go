// Package events sends call events: one record per call through the gateway, written off the
// request path. Events go into a bounded queue; a background worker writes them in batches to the
// configured sink. When the queue is full an event is dropped and counted: a request never waits.
package events

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

// Event is a call event (docs/data-schema/call_events.md). It carries identity and routing,
// never bodies, query values, headers, tokens or keys. A field that is not known is left out.
type Event struct {
	TS          time.Time `bson:"ts" json:"ts"`
	DurationMs  int       `bson:"durationMs" json:"durationMs"`
	RequestID   string    `bson:"requestId,omitempty" json:"requestId,omitempty"`
	Env         string    `bson:"env" json:"env"`
	Tenant      *Tenant   `bson:"tenant,omitempty" json:"tenant,omitempty"`
	Product     string    `bson:"product,omitempty" json:"product,omitempty"`
	Package     string    `bson:"package,omitempty" json:"package,omitempty"`
	AppID       string    `bson:"appId,omitempty" json:"appId,omitempty"`
	User        *User     `bson:"user,omitempty" json:"user,omitempty"`
	Service     string    `bson:"service,omitempty" json:"service,omitempty"`
	Version     string    `bson:"version,omitempty" json:"version,omitempty"`
	Method      string    `bson:"method" json:"method"`
	API         string    `bson:"api,omitempty" json:"api,omitempty"`
	MCPTool     string    `bson:"mcpTool,omitempty" json:"mcpTool,omitempty"`
	Status      int       `bson:"status" json:"status"`
	Code        int       `bson:"code,omitempty" json:"code,omitempty"`
	Idempotency string    `bson:"idempotency,omitempty" json:"idempotency,omitempty"`
	Cache       string    `bson:"cache,omitempty" json:"cache,omitempty"`
	IP          string    `bson:"ip,omitempty" json:"ip,omitempty"`
}

// Tenant is the tenant of the key: its id and code, and its main tenant for a sub-tenant.
type Tenant struct {
	ID   string     `bson:"id" json:"id"`
	Code string     `bson:"code" json:"code"`
	Main *TenantRef `bson:"main,omitempty" json:"main,omitempty"`
}

// TenantRef is a main tenant.
type TenantRef struct {
	ID   string `bson:"id" json:"id"`
	Code string `bson:"code" json:"code"`
}

// User is the signed-in user; Agent is true when the token is restricted (restrictedTo).
type User struct {
	ID       string `bson:"id,omitempty" json:"id,omitempty"`
	Username string `bson:"username,omitempty" json:"username,omitempty"`
	Agent    bool   `bson:"agent,omitempty" json:"agent,omitempty"`
}

// Sink writes a batch of events.
type Sink interface {
	Write(ctx context.Context, batch []*Event) error
}

// SinkResolver gives the sink to write to now, from the current configuration; nil without one.
type SinkResolver func() (Sink, error)

const (
	// DefaultQueueSize is the queue size when the configuration gives none.
	DefaultQueueSize = 10000
	// batchSize and flushInterval: a batch is written when it is full or the interval passed.
	batchSize     = 200
	flushInterval = time.Second
	writeTimeout  = 10 * time.Second
	// reportInterval: drops and sink failures are logged at most this often, with the totals
	reportInterval = time.Minute
)

// Emitter queues events and writes them in batches.
type Emitter struct {
	queue    chan *Event
	resolve  SinkResolver
	logger   *zap.Logger
	dropped  atomic.Int64
	written  atomic.Int64
	failures atomic.Int64
	// lastErr is the last sink error, logged with the next report (worker only)
	lastErr error

	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

// NewEmitter starts an emitter with a queue of size events (DefaultQueueSize when not positive).
func NewEmitter(size int, resolve SinkResolver, logger *zap.Logger) *Emitter {
	if size <= 0 {
		size = DefaultQueueSize
	}
	e := &Emitter{
		queue:   make(chan *Event, size),
		resolve: resolve,
		logger:  logger,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	go e.run()
	return e
}

// Emit queues an event without waiting; when the queue is full the event is dropped and counted.
func (e *Emitter) Emit(ev *Event) bool {
	select {
	case e.queue <- ev:
		return true
	default:
		e.dropped.Add(1)
		return false
	}
}

// Dropped is the number of events dropped, the queue being full or the sink failing.
func (e *Emitter) Dropped() int64 { return e.dropped.Load() }

// Failures is the number of batches the sink could not take.
func (e *Emitter) Failures() int64 { return e.failures.Load() }

// Written is the number of events the sink accepted.
func (e *Emitter) Written() int64 { return e.written.Load() }

// Close writes what is queued and stops the worker.
func (e *Emitter) Close() {
	e.stopOnce.Do(func() { close(e.stop) })
	<-e.done
}

func (e *Emitter) run() {
	defer close(e.done)
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()
	batch := make([]*Event, 0, batchSize)
	var reportedDropped, reportedFailures int64
	var lastReport time.Time
	// report logs the totals when they changed, at most once per reportInterval unless forced
	report := func(force bool) {
		d, f := e.dropped.Load(), e.failures.Load()
		if (d == reportedDropped && f == reportedFailures) || (!force && time.Since(lastReport) < reportInterval) {
			return
		}
		fields := []zap.Field{zap.Int64("dropped", d), zap.Int64("sinkFailures", f)}
		if e.lastErr != nil {
			fields = append(fields, zap.NamedError("lastError", e.lastErr))
		}
		e.logger.Error("call events dropped", fields...)
		reportedDropped, reportedFailures, lastReport = d, f, time.Now()
	}
	flush := func() {
		if len(batch) > 0 {
			e.write(batch)
			batch = make([]*Event, 0, batchSize)
		}
		report(false)
	}
	for {
		select {
		case ev := <-e.queue:
			batch = append(batch, ev)
			if len(batch) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-e.stop:
			for {
				select {
				case ev := <-e.queue:
					batch = append(batch, ev)
				default:
					flush()
					report(true)
					return
				}
			}
		}
	}
}

func (e *Emitter) write(batch []*Event) {
	sink, err := e.resolve()
	if err != nil || sink == nil {
		if err != nil {
			e.failures.Add(1)
			e.lastErr = err
		}
		e.dropped.Add(int64(len(batch)))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	if err := sink.Write(ctx, batch); err != nil {
		e.failures.Add(1)
		e.lastErr = err
		e.dropped.Add(int64(len(batch)))
		return
	}
	e.written.Add(int64(len(batch)))
}
