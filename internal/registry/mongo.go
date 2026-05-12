package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// MongoRegistry implements Registry using MongoDB
type MongoRegistry struct {
	client *mongo.Client
	logger *zap.Logger
	env    string

	mu   sync.RWMutex
	data *types.RegistryData
}

// NewMongoRegistry creates a new MongoDB-backed registry
func NewMongoRegistry(client *mongo.Client, logger *zap.Logger) *MongoRegistry {
	return &MongoRegistry{
		client: client,
		logger: logger,
	}
}

// Load loads the registry data for the given environment
func (r *MongoRegistry) Load(env string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.env = env
	return r.loadData()
}

// Reload reloads the registry data
func (r *MongoRegistry) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	return r.loadData()
}

func (r *MongoRegistry) loadData() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	data := &types.RegistryData{
		Services:       make(map[string]*types.ServiceConfig),
		Daemons:        make(map[string]*types.DaemonConfig),
		CustomRegistry: make(map[string]interface{}),
	}

	// Load environment configuration
	envColl := r.client.Database().Collection("environment")
	var envDoc types.EnvironmentDoc
	err := envColl.FindOne(ctx, bson.M{"code": r.env}).Decode(&envDoc)
	if err != nil {
		return fmt.Errorf("failed to load environment: %w", err)
	}

	data.ServiceConfig = &envDoc.ServiceConfig
	data.CoreDB = &envDoc.CoreDB

	// Load services
	servicesColl := r.client.Database().Collection("services")
	cursor, err := servicesColl.Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to load services: %w", err)
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var svc types.ServiceConfig
		if err := cursor.Decode(&svc); err != nil {
			r.logger.Warn("failed to decode service", zap.Error(err))
			continue
		}
		data.Services[svc.Name] = &svc
	}

	// Load daemons
	daemonsColl := r.client.Database().Collection("daemons")
	cursor, err = daemonsColl.Find(ctx, bson.M{})
	if err != nil {
		r.logger.Warn("failed to load daemons", zap.Error(err))
	} else {
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var daemon types.DaemonConfig
			if err := cursor.Decode(&daemon); err != nil {
				continue
			}
			data.Daemons[daemon.Name] = &daemon
		}
	}

	// Load custom registry
	customColl := r.client.Database().Collection("custom_registry")
	cursor, err = customColl.Find(ctx, bson.M{"env": r.env})
	if err != nil {
		r.logger.Warn("failed to load custom registry", zap.Error(err))
	} else {
		defer cursor.Close(ctx)
		for cursor.Next(ctx) {
			var custom bson.M
			if err := cursor.Decode(&custom); err != nil {
				continue
			}
			if name, ok := custom["name"].(string); ok {
				data.CustomRegistry[name] = custom["value"]
			}
		}
	}

	r.data = data
	r.logger.Info("registry loaded",
		zap.String("env", r.env),
		zap.Int("services", len(data.Services)),
		zap.Int("daemons", len(data.Daemons)),
	)

	return nil
}

// Get returns the current registry data
func (r *MongoRegistry) Get() *types.RegistryData {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data
}

// GetService returns service configuration
func (r *MongoRegistry) GetService(name string) (*types.ServiceConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.Services == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	svc, exists := r.data.Services[name]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", name)
	}

	return svc, nil
}

// RegisterHost registers a service host
func (r *MongoRegistry) RegisterHost(info types.HostInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hostsColl := r.client.Database().Collection("hosts")

	_, err := hostsColl.UpdateOne(
		ctx,
		bson.M{
			"env":     r.env,
			"name":    info.Name,
			"version": info.Version,
			"ip":      info.Host,
		},
		bson.M{
			"$set": bson.M{
				"port":      info.Port,
				"lastCheck": time.Now(),
				"healthy":   true,
			},
		},
		mongo.Upsert(),
	)

	return err
}

// GetHost returns a service host
func (r *MongoRegistry) GetHost(serviceName, version string) (*types.HostInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.Services == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	svc, exists := r.data.Services[serviceName]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	// Return first healthy host
	// TODO: Implement proper load balancing
	return &types.HostInfo{
		Name:    serviceName,
		Version: version,
		Host:    svc.Host,
		Port:    svc.Port,
	}, nil
}

// AddUpdateEnvControllers updates environment controller info
func (r *MongoRegistry) AddUpdateEnvControllers(data types.ControllerUpdate) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.client.Database().Collection("env_controllers")

	_, err := coll.UpdateOne(
		ctx,
		bson.M{
			"env": data.Env,
			"ip":  data.IP,
		},
		bson.M{
			"$set": bson.M{
				"ts":   data.TS,
				"data": data.Data,
			},
		},
		mongo.Upsert(),
	)

	return err
}
