package events

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

// blockingSink holds every write until released, and keeps what it was given.
type blockingSink struct {
	release chan struct{}
	mu      sync.Mutex
	got     []*Event
}

func (s *blockingSink) Write(_ context.Context, batch []*Event) error {
	<-s.release
	s.mu.Lock()
	s.got = append(s.got, batch...)
	s.mu.Unlock()
	return nil
}

func (s *blockingSink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.got)
}

// A full queue drops and counts; Emit never waits for the sink.
func TestEmitterOverflowDropsWithoutBlocking(t *testing.T) {
	sink := &blockingSink{release: make(chan struct{})}
	e := NewEmitter(4, func() (Sink, error) { return sink, nil }, zap.NewNop())
	const total = 1000
	start := time.Now()
	accepted := 0
	for i := 0; i < total; i++ {
		if e.Emit(&Event{Method: "GET", Status: 200}) {
			accepted++
		}
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Errorf("Emit waited: %v for %d events", elapsed, total)
	}
	if e.Dropped() == 0 || accepted+int(e.Dropped()) != total {
		t.Errorf("accepted %d, dropped %d, of %d", accepted, e.Dropped(), total)
	}
	close(sink.release)
	e.Close()
	if sink.count() != accepted || e.Written() != int64(accepted) {
		t.Errorf("written %d (%d), accepted %d", sink.count(), e.Written(), accepted)
	}
}

// Close writes what is queued.
func TestEmitterFlushesOnClose(t *testing.T) {
	sink := &blockingSink{release: make(chan struct{})}
	close(sink.release)
	e := NewEmitter(0, func() (Sink, error) { return sink, nil }, zap.NewNop())
	for i := 0; i < 450; i++ {
		e.Emit(&Event{Method: "GET", Status: 200})
	}
	e.Close()
	if sink.count() != 450 || e.Dropped() != 0 {
		t.Errorf("written %d, dropped %d", sink.count(), e.Dropped())
	}
}

type failingSink struct{}

func (failingSink) Write(context.Context, []*Event) error { return errors.New("down") }

// A failing sink, or none, drops the batch and counts it.
func TestEmitterSinkFailureCounts(t *testing.T) {
	e := NewEmitter(10, func() (Sink, error) { return failingSink{}, nil }, zap.NewNop())
	e.Emit(&Event{})
	e.Emit(&Event{})
	e.Close()
	if e.Dropped() != 2 || e.Written() != 0 {
		t.Errorf("dropped %d written %d", e.Dropped(), e.Written())
	}
	none := NewEmitter(10, func() (Sink, error) { return nil, nil }, zap.NewNop())
	none.Emit(&Event{})
	none.Close()
	if none.Dropped() != 1 {
		t.Errorf("no sink: dropped %d", none.Dropped())
	}
}

// A sink failing on every batch is logged once, not per batch, then once more with the totals on
// close.
func TestEmitterReportsAtMostOncePerInterval(t *testing.T) {
	core, logs := observer.New(zapcore.ErrorLevel)
	e := NewEmitter(1000, func() (Sink, error) { return failingSink{}, nil }, zap.New(core))
	for round := 0; round < 3; round++ {
		for i := 0; i < 450; i++ {
			e.Emit(&Event{})
		}
		time.Sleep(1200 * time.Millisecond)
	}
	if n := logs.Len(); n != 1 {
		t.Errorf("%d reports before close, want 1", n)
	}
	e.Close()
	entries := logs.All()
	if len(entries) != 2 {
		t.Fatalf("%d reports, want 2", len(entries))
	}
	last := entries[1].ContextMap()
	if last["dropped"] != int64(1350) || last["sinkFailures"].(int64) < 7 || last["lastError"] != "down" {
		t.Errorf("totals %v", last)
	}
}

// Many goroutines emit at once (go test -race).
func TestEmitterConcurrent(t *testing.T) {
	sink := &blockingSink{release: make(chan struct{})}
	close(sink.release)
	e := NewEmitter(100, func() (Sink, error) { return sink, nil }, zap.NewNop())
	var wg sync.WaitGroup
	for g := 0; g < 20; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				e.Emit(&Event{Method: "POST", Status: 201})
			}
		}()
	}
	wg.Wait()
	e.Close()
	if int64(sink.count())+e.Dropped() != 4000 {
		t.Errorf("written %d + dropped %d != 4000", sink.count(), e.Dropped())
	}
}

// The mongo sink on a local MongoDB (MONGO_URI, default mongodb://127.0.0.1:27017), in a
// conformance_ database it drops; skipped without one.
func TestMongoSink(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := drv.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(2*time.Second))
	if err == nil {
		err = client.Ping(ctx, nil)
	}
	if err != nil {
		t.Skip("no local MongoDB: " + err.Error())
	}
	defer client.Disconnect(context.Background())
	db := client.Database("conformance_call_events_test")
	_ = db.Drop(ctx)
	defer db.Drop(context.Background())

	sink, err := NewMongoSink(ctx, db, "call_events")
	if err != nil {
		t.Fatal(err)
	}
	// an existing collection is used as it is
	if _, err := NewMongoSink(ctx, db, "call_events"); err != nil {
		t.Fatalf("existing collection: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	batch := []*Event{
		{TS: now, DurationMs: 12, RequestID: "r1", Env: "dev",
			Tenant:  &Tenant{ID: "t1", Code: "T1", Main: &TenantRef{ID: "t0", Code: "T0"}},
			Product: "P", Package: "P_PKG", AppID: "a1",
			User:    &User{ID: "u1", Username: "agent1", Agent: true},
			Service: "orders", Version: "1", Method: "GET", API: "/orders/:id", MCPTool: "orders_getOrder",
			Status: 200, Idempotency: "replay", Cache: "HIT", IP: "10.0.0.1"},
		{TS: now, DurationMs: 0, Env: "dev", Method: "POST", Status: 403, Code: 154},
	}
	if err := sink.Write(ctx, batch); err != nil {
		t.Fatal(err)
	}
	var info bson.M
	if err := db.RunCommand(ctx, bson.D{{Key: "listCollections", Value: 1}, {Key: "filter", Value: bson.M{"name": "call_events"}}}).Decode(&info); err != nil {
		t.Fatal(err)
	}
	cursor, _ := info["cursor"].(bson.M)
	first, _ := cursor["firstBatch"].(bson.A)
	if len(first) != 1 || first[0].(bson.M)["type"] != "timeseries" {
		t.Errorf("not a time-series collection: %v", first)
	}
	schemas, err := dataschema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	cur, err := db.Collection("call_events").Find(ctx, bson.M{})
	if err != nil {
		t.Fatal(err)
	}
	defer cur.Close(ctx)
	n := 0
	for cur.Next(ctx) {
		n++
		if problems, err := schemas["call_events"].ValidateBSON(cur.Current); err != nil || problems != nil {
			t.Errorf("call_events does not match its schema: %v %v\n%s", problems, err, cur.Current)
		}
	}
	if n != 2 {
		t.Errorf("%d documents", n)
	}
}
