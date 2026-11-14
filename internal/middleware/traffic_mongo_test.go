package middleware

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// The mongo model on a local MongoDB (MONGO_URI, default mongodb://127.0.0.1:27017), in a
// conformance_ database it drops; skipped without one.
func TestMongoThrottling(t *testing.T) {
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
	db := client.Database("conformance_throttling_test")
	_ = db.Drop(ctx)
	defer db.Drop(context.Background())

	m := NewMongoThrottling(db, zap.NewNop())
	perIP := throttlingStrategy{perIP: true, window: 60000, limit: 1}
	k := trafficKey{"t1", "1.1.1.1"}
	if w := m.Get(perIP, k); w.Count != 0 {
		t.Fatalf("a new counter: %+v", w)
	}
	m.Increment(perIP, k)
	m.Increment(perIP, k)
	if w := m.Get(perIP, k); w.Count != 2 {
		t.Errorf("counted: %+v", w)
	}
	if w := m.Reset(perIP, k); w.Count != 0 {
		t.Errorf("reset: %+v", w)
	}
	// a strategy by tenant counts under l2 "_ALL_"
	m.Get(throttlingStrategy{}, k)
	if n, _ := db.Collection("throttling_monitor").CountDocuments(ctx, bson.M{"l1": "t1", "l2": "_ALL_"}); n != 1 {
		t.Errorf("_ALL_ documents: %d", n)
	}

	m.LogTooManyRequests(k, "heavy")
	waitFor(t, func() bool {
		n, _ := db.Collection("throttling_error").CountDocuments(ctx, bson.M{})
		return n == 1
	})
	schemas, err := dataschema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, coll := range []string{"throttling_monitor", "throttling_error"} {
		cur, err := db.Collection(coll).Find(ctx, bson.M{})
		if err != nil {
			t.Fatal(err)
		}
		for cur.Next(ctx) {
			if problems, err := schemas[coll].ValidateBSON(cur.Current); err != nil || problems != nil {
				t.Errorf("%s does not match its schema: %v %v", coll, problems, err)
			}
		}
		cur.Close(ctx)
	}
	var doc bson.M
	_ = db.Collection("throttling_error").FindOne(ctx, bson.M{}).Decode(&doc)
	if doc["staregy"] != "heavy" || doc["l2"] != "1.1.1.1" {
		t.Errorf("error document %v", doc)
	}
}
