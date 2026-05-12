package mongo

import (
	"context"
	"fmt"
	"time"

	"github.com/soajs/soajs.controller.go/internal/config"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Client wraps the MongoDB client
type Client struct {
	client   *mongo.Client
	database *mongo.Database
	cfg      config.MongoConfig
}

// NewClient creates a new MongoDB client
func NewClient(cfg config.MongoConfig) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ConnectTimeout)*time.Second)
	defer cancel()

	clientOpts := options.Client().
		ApplyURI(cfg.URI).
		SetMaxPoolSize(cfg.MaxPoolSize).
		SetMinPoolSize(cfg.MinPoolSize)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	return &Client{
		client:   client,
		database: client.Database(cfg.Database),
		cfg:      cfg,
	}, nil
}

// Database returns the database instance
func (c *Client) Database() *mongo.Database {
	return c.database
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
	return c.client.Ping(ctx, nil)
}

// Upsert returns upsert options
func Upsert() *options.UpdateOptions {
	return options.Update().SetUpsert(true)
}
