// Package data provides test data import utilities for integration tests.
package data

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ProvisionData holds all test data collections
type ProvisionData struct {
	Environment []bson.M `json:"environment"`
	Tenants     []bson.M `json:"tenants"`
	Products    []bson.M `json:"products"`
	Hosts       []bson.M `json:"hosts"`
	Resources   []bson.M `json:"resources"`
	Marketplace []bson.M `json:"marketplace"`
	OAuthToken  []bson.M `json:"oauth_token"`
}

// Import loads test data into MongoDB
func Import(ctx context.Context, mongoURI, database string) error {
	// Load provision data from JSON file
	data, err := loadProvisionData()
	if err != nil {
		return fmt.Errorf("failed to load provision data: %w", err)
	}

	// Connect to MongoDB
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	db := client.Database(database)

	// Clear and import each collection
	collections := map[string][]bson.M{
		"environment": data.Environment,
		"tenants":     data.Tenants,
		"products":    data.Products,
		"hosts":       data.Hosts,
		"resources":   data.Resources,
		"marketplace": data.Marketplace,
		"oauth_token": data.OAuthToken,
	}

	for name, docs := range collections {
		if err := importCollection(ctx, db, name, docs); err != nil {
			return fmt.Errorf("failed to import %s: %w", name, err)
		}
	}

	return nil
}

// Cleanup removes all test data from MongoDB
func Cleanup(ctx context.Context, mongoURI, database string) error {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer client.Disconnect(ctx)

	return client.Database(database).Drop(ctx)
}

func loadProvisionData() (*ProvisionData, error) {
	// Get the directory of this source file
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("failed to get current file path")
	}
	dir := filepath.Dir(filename)
	dataFile := filepath.Join(dir, "provision.json")

	file, err := os.ReadFile(dataFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read provision.json: %w", err)
	}

	var data ProvisionData
	if err := json.Unmarshal(file, &data); err != nil {
		return nil, fmt.Errorf("failed to parse provision.json: %w", err)
	}

	// Convert string _id fields to ObjectID
	convertIDs(data.Environment)
	convertIDs(data.Tenants)
	convertIDs(data.Products)
	convertIDs(data.Hosts)
	convertIDs(data.Resources)
	convertIDs(data.Marketplace)
	convertIDs(data.OAuthToken)

	return &data, nil
}

func convertIDs(docs []bson.M) {
	for i := range docs {
		if id, ok := docs[i]["_id"].(string); ok {
			if oid, err := primitive.ObjectIDFromHex(id); err == nil {
				docs[i]["_id"] = oid
			}
		}
		// Handle nested appId in tenants
		if apps, ok := docs[i]["applications"].([]interface{}); ok {
			for _, app := range apps {
				if appMap, ok := app.(map[string]interface{}); ok {
					if appId, ok := appMap["appId"].(string); ok {
						if oid, err := primitive.ObjectIDFromHex(appId); err == nil {
							appMap["appId"] = oid
						}
					}
				}
			}
		}
		// Handle expires date in oauth_token
		if expires, ok := docs[i]["expires"].(string); ok {
			if t, err := time.Parse(time.RFC3339, expires); err == nil {
				docs[i]["expires"] = t
			}
		}
	}
}

func importCollection(ctx context.Context, db *mongo.Database, name string, docs []bson.M) error {
	if len(docs) == 0 {
		return nil
	}

	coll := db.Collection(name)

	// Drop existing collection
	if err := coll.Drop(ctx); err != nil {
		// Ignore "ns not found" error
	}

	// Convert []bson.M to []interface{} for InsertMany
	documents := make([]interface{}, len(docs))
	for i, doc := range docs {
		documents[i] = doc
	}

	_, err := coll.InsertMany(ctx, documents)
	return err
}

// TestKey is the external key used in tests
const TestKey = "3d90163cf9d6b3076ad26aa5ed58556348069258e5c6c941ee0f18448b570ad1c5c790e2d2a1989680c55f4904e2005ff5f8e71606e4aa641e67882f4210ebbc5460ff305dcb36e6ec2a2299cf0448ef60b9e38f41950ec251c1cf41f05f3ce9"

// TestAccessToken is the OAuth access token used in tests
const TestAccessToken = "cfb209a91b23896820f510aadbf1f4284b512123"

// TestTenantID is the tenant ID used in tests
const TestTenantID = "5c0e74ba9acc3c5a84a51259"

// TestUserID is the user ID used in tests
const TestUserID = "5c8d0c505653de3985aa0ffd"
