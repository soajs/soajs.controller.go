package events

import (
	"context"
	"errors"

	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// MongoSink inserts events into a time-series collection (timeField ts, metaField tenant),
// created when it does not exist.
type MongoSink struct {
	coll *drv.Collection
}

// NewMongoSink returns the sink of a collection, creating it as a time-series collection.
func NewMongoSink(ctx context.Context, db *drv.Database, collection string) (*MongoSink, error) {
	ts := options.TimeSeries().SetTimeField("ts").SetMetaField("tenant").SetGranularity("seconds")
	err := db.CreateCollection(ctx, collection, options.CreateCollection().SetTimeSeriesOptions(ts))
	var cmdErr drv.CommandError
	if err != nil && !(errors.As(err, &cmdErr) && cmdErr.Code == 48) { // 48: NamespaceExists
		return nil, err
	}
	return &MongoSink{coll: db.Collection(collection)}, nil
}

func (s *MongoSink) Write(ctx context.Context, batch []*Event) error {
	docs := make([]interface{}, len(batch))
	for i, ev := range batch {
		docs[i] = ev
	}
	_, err := s.coll.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
	return err
}

// LogSink writes each event to the gateway log, for debugging.
type LogSink struct {
	Logger *zap.Logger
}

func (s LogSink) Write(_ context.Context, batch []*Event) error {
	for _, ev := range batch {
		s.Logger.Info("call event", zap.Any("event", ev))
	}
	return nil
}
