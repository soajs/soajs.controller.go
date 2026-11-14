package mongo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// ErrNotFound is returned when a document is not found
var ErrNotFound = mongo.ErrNoDocuments

// Client wraps a MongoDB client and the database it works on.
type Client struct {
	client   *mongo.Client
	database *mongo.Database
}

// Connect connects with opts, pings, and works on database. It retries three times.
func Connect(opts *options.ClientOptions, database string) (*Client, error) {
	var client *mongo.Client
	var err error
	opts.SetServerSelectionTimeout(5 * time.Second).SetConnectTimeout(10 * time.Second)
	for i := 0; i < 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		client, err = mongo.Connect(ctx, opts)
		if err == nil {
			err = client.Ping(ctx, readpref.Primary())
		}
		cancel()
		if err == nil {
			return &Client{client: client, database: client.Database(database)}, nil
		}
		time.Sleep(time.Duration(i+1) * time.Second)
	}
	return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
}

// WithDatabase returns a Client on the same connection working on another database.
func (c *Client) WithDatabase(name string) *Client {
	return &Client{client: c.client, database: c.client.Database(name)}
}

// Database returns the database instance
func (c *Client) Database() *mongo.Database {
	return c.database
}

// UseDatabase switches to a different database
func (c *Client) UseDatabase(name string) *mongo.Database {
	return c.client.Database(name)
}

// Collection returns a collection instance
func (c *Client) Collection(name string) *mongo.Collection {
	return c.database.Collection(name)
}

// Close closes the MongoDB connection
func (c *Client) Close(ctx context.Context) error {
	return c.client.Disconnect(ctx)
}

// Ping pings the MongoDB server
func (c *Client) Ping(ctx context.Context) error {
	return c.client.Ping(ctx, readpref.Primary())
}

// IsHealthy checks if the MongoDB connection is healthy
func (c *Client) IsHealthy() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return c.client.Ping(ctx, readpref.Primary()) == nil
}

// FindOne finds a single document in a collection
func (c *Client) FindOne(ctx context.Context, collection string, filter interface{}, result interface{}) error {
	return c.database.Collection(collection).FindOne(ctx, filter).Decode(result)
}

// FindMany finds multiple documents in a collection
func (c *Client) FindMany(ctx context.Context, collection string, filter interface{}, opts ...*options.FindOptions) (*mongo.Cursor, error) {
	return c.database.Collection(collection).Find(ctx, filter, opts...)
}

// InsertOne inserts a document
func (c *Client) InsertOne(ctx context.Context, collection string, doc interface{}) (*mongo.InsertOneResult, error) {
	return c.database.Collection(collection).InsertOne(ctx, doc)
}

// UpdateOne updates a document
func (c *Client) UpdateOne(ctx context.Context, collection string, filter, update interface{}, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return c.database.Collection(collection).UpdateOne(ctx, filter, update, opts...)
}

// DeleteOne deletes a document
func (c *Client) DeleteOne(ctx context.Context, collection string, filter interface{}) (*mongo.DeleteResult, error) {
	return c.database.Collection(collection).DeleteOne(ctx, filter)
}

// CountDocuments counts documents matching filter
func (c *Client) CountDocuments(ctx context.Context, collection string, filter interface{}) (int64, error) {
	return c.database.Collection(collection).CountDocuments(ctx, filter)
}

// Aggregate performs an aggregation pipeline
func (c *Client) Aggregate(ctx context.Context, collection string, pipeline interface{}) (*mongo.Cursor, error) {
	return c.database.Collection(collection).Aggregate(ctx, pipeline)
}

// CreateIndex creates an index on a collection
func (c *Client) CreateIndex(ctx context.Context, collection string, keys bson.D, unique bool) (string, error) {
	indexModel := mongo.IndexModel{
		Keys:    keys,
		Options: options.Index().SetUnique(unique),
	}
	return c.database.Collection(collection).Indexes().CreateOne(ctx, indexModel)
}

// WithTransaction executes a function within a transaction
func (c *Client) WithTransaction(ctx context.Context, fn func(ctx mongo.SessionContext) error) error {
	session, err := c.client.StartSession()
	if err != nil {
		return fmt.Errorf("failed to start session: %w", err)
	}
	defer session.EndSession(ctx)

	_, err = session.WithTransaction(ctx, func(sessCtx mongo.SessionContext) (interface{}, error) {
		return nil, fn(sessCtx)
	})
	return err
}

// Upsert returns upsert options
func Upsert() *options.UpdateOptions {
	return options.Update().SetUpsert(true)
}

// IsNotFound checks if an error is a not found error
func IsNotFound(err error) bool {
	return errors.Is(err, mongo.ErrNoDocuments)
}
