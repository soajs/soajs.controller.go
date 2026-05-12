package registry

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// Collection names
const (
	CollEnvironment    = "environment"
	CollHosts          = "hosts"
	CollResources      = "resources"
	CollCustomRegistry = "custom_registry"
	CollMarketplace    = "marketplace"
	CollControllers    = "controllers"
)

// MongoRegistry implements Registry using MongoDB
type MongoRegistry struct {
	client *mongo.Client
	logger *zap.Logger
	env    string
	isHA   bool

	mu    sync.RWMutex
	data  *types.RegistryData
	cache *Cache
}

// NewMongoRegistry creates a new MongoDB-backed registry
func NewMongoRegistry(client *mongo.Client, logger *zap.Logger) *MongoRegistry {
	return &MongoRegistry{
		client: client,
		logger: logger,
		isHA:   os.Getenv("SOAJS_DEPLOY_HA") != "",
		cache:  NewCache(5 * time.Minute),
	}
}

// Load loads the registry data for the given environment
func (r *MongoRegistry) Load(env string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.env = strings.ToUpper(env)
	return r.loadData()
}

// Reload reloads the registry data
func (r *MongoRegistry) Reload() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache.Clear()
	return r.loadData()
}

func (r *MongoRegistry) loadData() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	data := types.NewRegistryData(r.env)

	// Load environment configuration
	if err := r.loadEnvironment(ctx, data); err != nil {
		return fmt.Errorf("failed to load environment: %w", err)
	}

	// Load in parallel for better performance
	var wg sync.WaitGroup
	errChan := make(chan error, 5)

	// Load resources
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.loadResources(ctx, data); err != nil {
			errChan <- fmt.Errorf("failed to load resources: %w", err)
		}
	}()

	// Load custom registry
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.loadCustomRegistry(ctx, data); err != nil {
			errChan <- fmt.Errorf("failed to load custom registry: %w", err)
		}
	}()

	// Load marketplace services
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.loadMarketplaceServices(ctx, data); err != nil {
			errChan <- fmt.Errorf("failed to load marketplace services: %w", err)
		}
	}()

	// Load marketplace daemons
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := r.loadMarketplaceDaemons(ctx, data); err != nil {
			errChan <- fmt.Errorf("failed to load marketplace daemons: %w", err)
		}
	}()

	// Load hosts (only if not in HA mode)
	if !r.isHA {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := r.loadHosts(ctx, data); err != nil {
				errChan <- fmt.Errorf("failed to load hosts: %w", err)
			}
		}()
	}

	wg.Wait()
	close(errChan)

	// Collect errors
	var errors []error
	for err := range errChan {
		errors = append(errors, err)
		r.logger.Warn("registry load warning", zap.Error(err))
	}

	r.data = data
	r.logger.Info("registry loaded",
		zap.String("env", r.env),
		zap.Int("services", len(data.Services)),
		zap.Int("daemons", len(data.Daemons)),
		zap.Int("resources", countResources(data.Resources)),
		zap.Int("custom", len(data.CustomRegistry)),
	)

	return nil
}

func (r *MongoRegistry) loadEnvironment(ctx context.Context, data *types.RegistryData) error {
	coll := r.client.Collection(CollEnvironment)
	var envDoc types.EnvironmentDoc
	err := coll.FindOne(ctx, bson.M{"code": r.env}).Decode(&envDoc)
	if err != nil {
		return err
	}

	data.ServiceConfig = &envDoc.ServiceConfig
	data.CoreDB = &envDoc.CoreDB
	return nil
}

func (r *MongoRegistry) loadResources(ctx context.Context, data *types.RegistryData) error {
	coll := r.client.Collection(CollResources)

	// Build query for resources available to this environment
	criteria := bson.M{
		"$or": []bson.M{
			{
				"created": r.env,
				"plugged": true,
			},
			{
				"plugged": true,
				"shared":  true,
			},
		},
	}

	cursor, err := coll.Find(ctx, criteria)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var resource types.Resource
		if err := cursor.Decode(&resource); err != nil {
			r.logger.Warn("failed to decode resource", zap.Error(err))
			continue
		}

		// Check if resource is available for this environment
		if !r.isResourceAvailable(&resource) {
			continue
		}

		// Group resources by type
		if data.Resources[resource.Type] == nil {
			data.Resources[resource.Type] = make(map[string]*types.Resource)
		}
		data.Resources[resource.Type][resource.Name] = &resource
	}

	return nil
}

func (r *MongoRegistry) isResourceAvailable(resource *types.Resource) bool {
	// Resource is available if:
	// 1. It was created in this environment
	// 2. Or it's shared and this environment is in sharedEnvs
	if resource.Created == r.env {
		return true
	}
	if resource.SharedEnvs != nil {
		return resource.SharedEnvs[r.env]
	}
	return false
}

func (r *MongoRegistry) loadCustomRegistry(ctx context.Context, data *types.RegistryData) error {
	coll := r.client.Collection(CollCustomRegistry)

	criteria := bson.M{
		"$or": []bson.M{
			{
				"created": r.env,
				"plugged": true,
			},
			{
				"plugged": true,
				"shared":  true,
			},
		},
	}

	cursor, err := coll.Find(ctx, criteria)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var entry types.CustomEntry
		if err := cursor.Decode(&entry); err != nil {
			r.logger.Warn("failed to decode custom entry", zap.Error(err))
			continue
		}

		// Check if entry is available for this environment
		if entry.Created == r.env || (entry.SharedEnvs != nil && entry.SharedEnvs[r.env]) {
			data.CustomRegistry[entry.Name] = &entry
		}
	}

	return nil
}

func (r *MongoRegistry) loadMarketplaceServices(ctx context.Context, data *types.RegistryData) error {
	coll := r.client.Collection(CollMarketplace)

	criteria := bson.M{
		"$or": []bson.M{
			{"type": "service"},
			{"type": "endpoint"},
		},
	}

	cursor, err := coll.Find(ctx, criteria)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var item types.MarketplaceItem
		if err := cursor.Decode(&item); err != nil {
			r.logger.Warn("failed to decode marketplace service", zap.Error(err))
			continue
		}

		svc := r.marketplaceToServiceConfig(&item)
		data.Services[svc.Name] = svc
	}

	return nil
}

func (r *MongoRegistry) loadMarketplaceDaemons(ctx context.Context, data *types.RegistryData) error {
	coll := r.client.Collection(CollMarketplace)

	cursor, err := coll.Find(ctx, bson.M{"type": "mdaemon"})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var item types.MarketplaceItem
		if err := cursor.Decode(&item); err != nil {
			r.logger.Warn("failed to decode marketplace daemon", zap.Error(err))
			continue
		}

		daemon := &types.DaemonConfig{
			ID:   item.ID,
			Name: item.Name,
		}
		if item.Configuration != nil {
			daemon.Port = item.Configuration.Port
			daemon.Group = item.Configuration.Group
		}
		if len(item.Versions) > 0 {
			daemon.Version = item.Versions[0].Version
		}
		data.Daemons[daemon.Name] = daemon
	}

	return nil
}

func (r *MongoRegistry) loadHosts(ctx context.Context, data *types.RegistryData) error {
	coll := r.client.Collection(CollHosts)

	cursor, err := coll.Find(ctx, bson.M{"env": strings.ToLower(r.env)})
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)

	for cursor.Next(ctx) {
		var host types.HostRecord
		if err := cursor.Decode(&host); err != nil {
			r.logger.Warn("failed to decode host", zap.Error(err))
			continue
		}

		key := host.Name
		if data.Hosts[key] == nil {
			data.Hosts[key] = []*types.HostRecord{}
		}
		data.Hosts[key] = append(data.Hosts[key], &host)
	}

	return nil
}

func (r *MongoRegistry) marketplaceToServiceConfig(item *types.MarketplaceItem) *types.ServiceConfig {
	svc := &types.ServiceConfig{
		ID:       item.ID,
		Name:     item.Name,
		Versions: make(map[string]*types.VersionInfo),
	}

	if item.Configuration != nil {
		svc.Port = item.Configuration.Port
		svc.Group = item.Configuration.Group
		svc.RequestTimeout = item.Configuration.RequestTimeout
		svc.RequestTimeoutRenewal = item.Configuration.RequestTimeoutRenewal
	}

	for _, v := range item.Versions {
		vInfo := &types.VersionInfo{
			ExtKeyRequired: v.ExtKeyRequired,
			OAuth:          v.OAuth,
			APIs:           v.APIs,
		}
		svc.Versions[v.Version] = vInfo
	}

	return svc
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

// GetCustomRegistry returns a custom registry entry
func (r *MongoRegistry) GetCustomRegistry(name string) (*types.CustomEntry, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.CustomRegistry == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	entry, exists := r.data.CustomRegistry[name]
	if !exists {
		return nil, fmt.Errorf("custom registry entry not found: %s", name)
	}

	return entry, nil
}

// GetResource returns a resource by type and name
func (r *MongoRegistry) GetResource(resourceType, name string) (*types.Resource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.Resources == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	typeResources, exists := r.data.Resources[resourceType]
	if !exists {
		return nil, fmt.Errorf("resource type not found: %s", resourceType)
	}

	resource, exists := typeResources[name]
	if !exists {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, name)
	}

	return resource, nil
}

// RegisterHost registers a service host
func (r *MongoRegistry) RegisterHost(info types.HostInfo) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := r.client.Collection(CollHosts)

	filter := bson.M{
		"env":     strings.ToLower(r.env),
		"name":    info.Name,
		"version": info.Version,
		"ip":      info.Host,
	}

	update := bson.M{
		"$set": bson.M{
			"port":      info.Port,
			"lastCheck": time.Now(),
			"healthy":   true,
		},
	}

	_, err := coll.UpdateOne(ctx, filter, update, mongo.Upsert())
	return err
}

// GetHost returns a service host
func (r *MongoRegistry) GetHost(serviceName, version string) (*types.HostInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// First try to get from hosts
	if r.data != nil && r.data.Hosts != nil {
		hosts, exists := r.data.Hosts[serviceName]
		if exists && len(hosts) > 0 {
			// Find a healthy host with matching version
			for _, h := range hosts {
				if h.Healthy && (version == "" || h.Version == version) {
					return &types.HostInfo{
						Name:    h.Name,
						Version: h.Version,
						Host:    h.IP,
						Port:    h.Port,
					}, nil
				}
			}
		}
	}

	// Fall back to service config
	if r.data == nil || r.data.Services == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	svc, exists := r.data.Services[serviceName]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

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

	coll := r.client.Collection(CollControllers)

	filter := bson.M{
		"env": strings.ToLower(data.Env),
		"ip":  data.IP,
	}

	update := bson.M{
		"$set": bson.M{
			"ts":   data.TS,
			"data": data.Data,
		},
	}

	_, err := coll.UpdateOne(ctx, filter, update, mongo.Upsert())
	return err
}

// IsMaintenanceMode checks if the system is in maintenance mode
func (r *MongoRegistry) IsMaintenanceMode() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.ServiceConfig == nil {
		return false
	}
	return r.data.ServiceConfig.MaintenanceMode
}

// GetEnvironment returns the current environment code
func (r *MongoRegistry) GetEnvironment() string {
	return r.env
}

func countResources(resources map[string]map[string]*types.Resource) int {
	count := 0
	for _, typeResources := range resources {
		count += len(typeResources)
	}
	return count
}
