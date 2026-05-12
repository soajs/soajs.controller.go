package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// FindOne finds a single document
func FindOne[T any](ctx context.Context, coll *mongo.Collection, filter interface{}) (*T, error) {
	var result T
	err := coll.FindOne(ctx, filter).Decode(&result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// FindMany finds multiple documents
func FindMany[T any](ctx context.Context, coll *mongo.Collection, filter interface{}, opts ...*options.FindOptions) ([]T, error) {
	cursor, err := coll.Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []T
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// InsertOne inserts a single document
func InsertOne(ctx context.Context, coll *mongo.Collection, doc interface{}) (interface{}, error) {
	result, err := coll.InsertOne(ctx, doc)
	if err != nil {
		return nil, err
	}
	return result.InsertedID, nil
}

// InsertMany inserts multiple documents
func InsertMany(ctx context.Context, coll *mongo.Collection, docs []interface{}) ([]interface{}, error) {
	result, err := coll.InsertMany(ctx, docs)
	if err != nil {
		return nil, err
	}
	return result.InsertedIDs, nil
}

// UpdateOne updates a single document
func UpdateOne(ctx context.Context, coll *mongo.Collection, filter, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return coll.UpdateOne(ctx, filter, update, opts...)
}

// UpdateMany updates multiple documents
func UpdateMany(ctx context.Context, coll *mongo.Collection, filter, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return coll.UpdateMany(ctx, filter, update, opts...)
}

// DeleteOne deletes a single document
func DeleteOne(ctx context.Context, coll *mongo.Collection, filter interface{}) (*mongo.DeleteResult, error) {
	return coll.DeleteOne(ctx, filter)
}

// DeleteMany deletes multiple documents
func DeleteMany(ctx context.Context, coll *mongo.Collection, filter interface{}) (*mongo.DeleteResult, error) {
	return coll.DeleteMany(ctx, filter)
}

// Count counts documents matching filter
func Count(ctx context.Context, coll *mongo.Collection, filter interface{}) (int64, error) {
	return coll.CountDocuments(ctx, filter)
}

// Exists checks if a document exists
func Exists(ctx context.Context, coll *mongo.Collection, filter interface{}) (bool, error) {
	count, err := coll.CountDocuments(ctx, filter, options.Count().SetLimit(1))
	return count > 0, err
}

// Aggregate performs an aggregation
func Aggregate[T any](ctx context.Context, coll *mongo.Collection, pipeline interface{}) ([]T, error) {
	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []T
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// EnsureIndex creates an index if it doesn't exist
func EnsureIndex(ctx context.Context, coll *mongo.Collection, keys bson.D, unique bool) error {
	indexModel := mongo.IndexModel{
		Keys:    keys,
		Options: options.Index().SetUnique(unique),
	}
	_, err := coll.Indexes().CreateOne(ctx, indexModel)
	return err
}
