package middleware

import (
	"context"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"go.mongodb.org/mongo-driver/bson"
	mongoDB "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"
)

// Collection names for throttling
const (
	CollThrottlingMonitor = "throttling_monitor"
	CollThrottlingError   = "throttling_error"
)

// MongoThrottlingModel implements ThrottlingModel using MongoDB storage
type MongoThrottlingModel struct {
	client      *mongo.Client
	database    *mongoDB.Database
	logger      *zap.Logger
	indexesOnce sync.Once
	indexesErr  error
}

// NewMongoThrottlingModel creates a new MongoDB-based throttling model
// If database is empty, it uses the client's default database
func NewMongoThrottlingModel(client *mongo.Client, database string, logger *zap.Logger) *MongoThrottlingModel {
	var db *mongoDB.Database
	if database != "" {
		db = client.UseDatabase(database)
	} else {
		db = client.Database()
	}

	m := &MongoThrottlingModel{
		client:   client,
		database: db,
		logger:   logger,
	}
	// Ensure indexes are created
	m.ensureIndexes()
	return m
}

// collection returns the collection from the throttling database
func (m *MongoThrottlingModel) collection(name string) *mongoDB.Collection {
	return m.database.Collection(name)
}

// ensureIndexes creates the required indexes for throttling collections
func (m *MongoThrottlingModel) ensureIndexes() {
	m.indexesOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Create index on throttling_monitor: { l1: 1, l2: 1 }
		monitorColl := m.collection(CollThrottlingMonitor)
		indexModel := mongoDB.IndexModel{
			Keys:    bson.D{{Key: "l1", Value: 1}, {Key: "l2", Value: 1}},
			Options: options.Index().SetUnique(true),
		}
		_, err := monitorColl.Indexes().CreateOne(ctx, indexModel)
		if err != nil {
			m.logger.Warn("failed to create throttling_monitor index", zap.Error(err))
			m.indexesErr = err
		} else {
			m.logger.Info("throttling_monitor index created",
				zap.String("database", m.database.Name()),
				zap.String("collection", CollThrottlingMonitor))
		}

		// Create index on throttling_error: { timestamp: -1 } with TTL
		errorColl := m.collection(CollThrottlingError)
		ttlIndexModel := mongoDB.IndexModel{
			Keys:    bson.D{{Key: "timestamp", Value: -1}},
			Options: options.Index().SetExpireAfterSeconds(86400 * 7), // 7 days TTL
		}
		_, err = errorColl.Indexes().CreateOne(ctx, ttlIndexModel)
		if err != nil {
			m.logger.Warn("failed to create throttling_error TTL index", zap.Error(err))
			m.indexesErr = err
		} else {
			m.logger.Info("throttling_error index created",
				zap.String("database", m.database.Name()),
				zap.String("collection", CollThrottlingError))
		}
	})
}

// GetThrottling retrieves the current throttling entry for a key
func (m *MongoThrottlingModel) GetThrottling(key TrafficKey) (*ThrottlingModelEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"l1": key.L1,
		"l2": key.L2,
	}

	var entry ThrottlingModelEntry
	err := m.collection(CollThrottlingMonitor).FindOne(ctx, filter).Decode(&entry)
	if err != nil {
		if mongo.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}

	return &entry, nil
}

// ResetThrottling resets the throttling counter for a key
func (m *MongoThrottlingModel) ResetThrottling(key TrafficKey) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"l1": key.L1,
		"l2": key.L2,
	}

	update := bson.M{
		"$set": bson.M{
			"count":        1,
			"firstReqTime": time.Now(),
		},
	}

	_, err := m.collection(CollThrottlingMonitor).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// IncrementThrottling increments the request count for a key
// Uses atomic $inc operation for thread-safety across pods
func (m *MongoThrottlingModel) IncrementThrottling(key TrafficKey) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"l1": key.L1,
		"l2": key.L2,
	}

	// Use $inc for atomic increment, $setOnInsert for new entries
	update := bson.M{
		"$inc": bson.M{
			"count": 1,
		},
		"$setOnInsert": bson.M{
			"l1":           key.L1,
			"l2":           key.L2,
			"firstReqTime": time.Now(),
		},
	}

	_, err := m.collection(CollThrottlingMonitor).UpdateOne(ctx, filter, update, options.Update().SetUpsert(true))
	return err
}

// LogTooManyRequests logs a rejected request to the error collection
func (m *MongoThrottlingModel) LogTooManyRequests(key TrafficKey, strategy string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	entry := ThrottlingErrorEntry{
		L1:        key.L1,
		L2:        key.L2,
		Strategy:  strategy,
		Timestamp: time.Now(),
	}

	_, err := m.collection(CollThrottlingError).InsertOne(ctx, entry)
	if err != nil {
		m.logger.Warn("failed to log throttling error", zap.Error(err))
	}
	return err
}

// Cleanup removes expired entries from throttling_monitor
func (m *MongoThrottlingModel) Cleanup(maxAge time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cutoff := time.Now().Add(-maxAge)
	filter := bson.M{
		"firstReqTime": bson.M{"$lt": cutoff},
	}

	result, err := m.collection(CollThrottlingMonitor).DeleteMany(ctx, filter)
	if err != nil {
		m.logger.Warn("failed to cleanup old throttling entries", zap.Error(err))
		return
	}

	if result.DeletedCount > 0 {
		m.logger.Debug("cleaned up old throttling entries",
			zap.Int64("deleted", result.DeletedCount))
	}
}

// GetOrCreateEntry gets an existing entry or creates a new one atomically
// This is a helper for the traffic middleware to get current state
func (m *MongoThrottlingModel) GetOrCreateEntry(key TrafficKey, windowMs int) (*ThrottlingModelEntry, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now()
	windowDuration := time.Duration(windowMs) * time.Millisecond

	filter := bson.M{
		"l1": key.L1,
		"l2": key.L2,
	}

	// First, try to find existing entry
	var entry ThrottlingModelEntry
	err := m.collection(CollThrottlingMonitor).FindOne(ctx, filter).Decode(&entry)
	if err != nil && !mongo.IsNotFound(err) {
		return nil, false, err
	}

	// Check if entry exists and if window expired
	if err == nil {
		// Entry exists, check window
		if now.Sub(entry.FirstReqTime) >= windowDuration {
			// Window expired, reset
			resetUpdate := bson.M{
				"$set": bson.M{
					"count":        1,
					"firstReqTime": now,
				},
			}
			_, updateErr := m.collection(CollThrottlingMonitor).UpdateOne(ctx, filter, resetUpdate)
			if updateErr != nil {
				return nil, false, updateErr
			}
			entry.Count = 1
			entry.FirstReqTime = now
			return &entry, true, nil
		}

		// Window still active, increment count atomically
		incrementUpdate := bson.M{
			"$inc": bson.M{"count": 1},
		}

		// Use FindOneAndUpdate to get the new count
		opts := options.FindOneAndUpdate().SetReturnDocument(options.After)
		result := m.collection(CollThrottlingMonitor).FindOneAndUpdate(ctx, filter, incrementUpdate, opts)
		if err := result.Decode(&entry); err != nil {
			return nil, false, err
		}

		return &entry, false, nil
	}

	// Entry doesn't exist, create new one
	newEntry := ThrottlingModelEntry{
		L1:           key.L1,
		L2:           key.L2,
		FirstReqTime: now,
		Count:        1,
	}

	_, err = m.collection(CollThrottlingMonitor).InsertOne(ctx, newEntry)
	if err != nil {
		// Check for duplicate key error (race condition - another pod created it)
		if mongoDB.IsDuplicateKeyError(err) {
			// Retry get
			return m.GetOrCreateEntry(key, windowMs)
		}
		return nil, false, err
	}

	return &newEntry, false, nil
}

// GetRemainingTime returns the remaining time in the current window
func (m *MongoThrottlingModel) GetRemainingTime(key TrafficKey, windowMs int) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	filter := bson.M{
		"l1": key.L1,
		"l2": key.L2,
	}

	var entry ThrottlingModelEntry
	err := m.collection(CollThrottlingMonitor).FindOne(ctx, filter).Decode(&entry)
	if err != nil {
		if mongo.IsNotFound(err) {
			return 0, nil
		}
		return 0, err
	}

	windowDuration := time.Duration(windowMs) * time.Millisecond
	elapsed := time.Since(entry.FirstReqTime)
	remaining := windowDuration - elapsed

	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}
