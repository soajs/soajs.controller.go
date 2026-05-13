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
	envDoc, err := r.loadEnvironment(ctx, data)
	if err != nil {
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

	// Build CoreDB and TenantMetaDB from dbs and resources
	r.buildDatabaseConfigs(data, envDoc)

	// Log hosts loaded before merge
	r.logger.Info("hosts loaded from MongoDB",
		zap.Int("hostServices", len(data.Hosts)),
	)

	// Merge hosts into services (like Node.js does)
	r.mergeHostsIntoServices(data)

	// Count services with hosts
	hostsCount := 0
	for _, svc := range data.Services {
		if svc.Hosts != nil && len(svc.Hosts.ByVersion) > 0 {
			hostsCount++
		}
	}

	r.data = data
	r.logger.Info("registry loaded",
		zap.String("env", r.env),
		zap.Int("services", len(data.Services)),
		zap.Int("servicesWithHosts", hostsCount),
		zap.Int("daemons", len(data.Daemons)),
		zap.Int("resources", countResources(data.Resources)),
		zap.Int("custom", len(data.CustomRegistry)),
	)

	return nil
}

func (r *MongoRegistry) loadEnvironment(ctx context.Context, data *types.RegistryData) (*types.EnvironmentDoc, error) {
	coll := r.client.Collection(CollEnvironment)
	var envDoc types.EnvironmentDoc
	err := coll.FindOne(ctx, bson.M{"code": r.env}).Decode(&envDoc)
	if err != nil {
		return nil, err
	}

	// Copy basic environment fields
	data.Domain = envDoc.Domain
	data.SitePrefix = envDoc.SitePrefix
	data.APIPrefix = envDoc.APIPrefix
	data.Protocol = envDoc.Protocol
	data.Port = envDoc.Port
	data.Environment = strings.ToLower(r.env)
	data.Name = strings.ToLower(r.env)

	// Set ServiceConfig from services.config
	data.ServiceConfig = &envDoc.ServiceConfig.Config

	// Set Deployer
	data.Deployer = envDoc.Deployer

	// Initialize empty CoreDB - will be populated after resources are loaded
	data.CoreDB = &types.CoreDBConfig{}

	return &envDoc, nil
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

func (r *MongoRegistry) buildDatabaseConfigs(data *types.RegistryData, envDoc *types.EnvironmentDoc) {
	if envDoc == nil {
		return
	}

	// Get the database prefix
	dbPrefix := envDoc.DBs.Config.Prefix

	// Build TenantMetaDB and CoreDB from dbs.databases
	data.TenantMetaDB = make(map[string]*types.DBConnectionConfig)

	for dbName, dbRef := range envDoc.DBs.Databases {
		// Get cluster resource
		clusterResource := r.getClusterResource(data, dbRef.Cluster)
		if clusterResource == nil {
			r.logger.Warn("cluster resource not found",
				zap.String("cluster", dbRef.Cluster),
				zap.String("database", dbName),
			)
			continue
		}

		// Build connection config from cluster resource
		connConfig := r.buildConnectionConfigFromResource(clusterResource, dbName, dbPrefix)

		if dbRef.TenantSpecific {
			// Tenant-specific databases use #TENANT_NAME#_dbName pattern
			connConfig.Name = "#TENANT_NAME#_" + dbName
			data.TenantMetaDB[dbName] = connConfig
		} else {
			// Non-tenant-specific core databases
			switch dbName {
			case "provision":
				data.CoreDB.Provision = *connConfig
			case "oauth":
				data.CoreDB.OAuth = *connConfig
			case "gateway":
				data.CoreDB.Gateway = *connConfig
			}
		}
	}

	// Set default provision database if not set (using first cluster found)
	// Pass empty dbName to use cluster's config.name (e.g., "core_provision")
	if data.CoreDB.Provision.Cluster == "" && len(data.Resources["cluster"]) > 0 {
		for _, res := range data.Resources["cluster"] {
			provConfig := r.buildConnectionConfigFromResource(res, "", dbPrefix)
			data.CoreDB.Provision = *provConfig
			data.CoreDB.OAuth = *provConfig
			data.CoreDB.OAuth.Name = dbPrefix + "oauth"
			break
		}
	}
}

func (r *MongoRegistry) getClusterResource(data *types.RegistryData, clusterName string) *types.Resource {
	if data.Resources == nil || data.Resources["cluster"] == nil {
		return nil
	}
	return data.Resources["cluster"][clusterName]
}

func (r *MongoRegistry) buildConnectionConfigFromResource(resource *types.Resource, dbName, prefix string) *types.DBConnectionConfig {
	config := &types.DBConnectionConfig{
		Cluster: resource.Name,
		Name:    prefix + dbName,
		Prefix:  prefix,
	}

	// Extract config fields from resource.Config
	if resource.Config != nil {
		// Servers - handle both []interface{} and bson.A (primitive.A)
		var servers []interface{}
		if s, ok := resource.Config["servers"].([]interface{}); ok {
			servers = s
		} else if s, ok := resource.Config["servers"].(bson.A); ok {
			servers = s
		}

		for _, srv := range servers {
			var srvMap map[string]interface{}
			if m, ok := srv.(map[string]interface{}); ok {
				srvMap = m
			} else if m, ok := srv.(bson.M); ok {
				srvMap = m
			}
			if srvMap != nil {
				host, _ := srvMap["host"].(string)
				port := getIntValue(srvMap["port"])
				config.Servers = append(config.Servers, types.ServerConfig{
					Host: host,
					Port: port,
				})
			}
		}

		// URLParam - handle both map types
		var urlParam map[string]interface{}
		if u, ok := resource.Config["URLParam"].(map[string]interface{}); ok {
			urlParam = u
		} else if u, ok := resource.Config["URLParam"].(bson.M); ok {
			urlParam = u
		}
		if urlParam != nil {
			config.URLParam = make(map[string]string)
			for k, v := range urlParam {
				if str, ok := v.(string); ok {
					config.URLParam[k] = str
				}
			}
		}

		// Credentials - handle both map types
		var creds map[string]interface{}
		if c, ok := resource.Config["credentials"].(map[string]interface{}); ok {
			creds = c
		} else if c, ok := resource.Config["credentials"].(bson.M); ok {
			creds = c
		}
		if creds != nil {
			config.Credentials = types.CredentialsConfig{
				Username: getString(creds, "username"),
				Password: getString(creds, "password"),
			}
		}

		// Streaming
		config.Streaming = resource.Config["streaming"]

		// Name from resource if dbName wasn't provided
		if name, ok := resource.Config["name"].(string); ok && dbName == "" {
			config.Name = name
		}

		// Prefix from resource if present
		if p, ok := resource.Config["prefix"].(string); ok && prefix == "" {
			config.Prefix = p
		}
	}

	return config
}

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getIntValue(v interface{}) int {
	switch p := v.(type) {
	case float64:
		return int(p)
	case int:
		return p
	case int32:
		return int(p)
	case int64:
		return int(p)
	default:
		return 0
	}
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
		var entry bson.M
		if err := cursor.Decode(&entry); err != nil {
			r.logger.Warn("failed to decode custom entry", zap.Error(err))
			continue
		}

		// Get entry name and check if available for this environment
		name, _ := entry["name"].(string)
		created, _ := entry["created"].(string)
		sharedEnvs, _ := entry["sharedEnvs"].(bson.M)

		if created == r.env || (sharedEnvs != nil && sharedEnvs[r.env] != nil) {
			// Convert to proper format matching Node.js output
			data.CustomRegistry[name] = entry
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
func (r *MongoRegistry) GetCustomRegistry(name string) (interface{}, error) {
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

	// Get hostname like Node.js does
	hostname, _ := os.Hostname()
	hostname = strings.ToLower(hostname)

	filter := bson.M{
		"env":      strings.ToLower(r.env),
		"name":     info.Name,
		"version":  info.Version,
		"ip":       info.Host,
		"hostname": hostname,
	}

	update := bson.M{
		"$set": bson.M{
			"type":      "service",
			"env":       strings.ToLower(r.env),
			"name":      info.Name,
			"ip":        info.Host,
			"port":      info.Port,
			"hostname":  hostname,
			"version":   info.Version,
			"lastCheck": time.Now(),
			"healthy":   true,
		},
	}

	_, err := coll.UpdateOne(ctx, filter, update, mongo.Upsert())
	if err != nil {
		return err
	}

	// Also update in-memory state so awarenessStat reflects the registration immediately
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.data != nil && r.data.Services != nil {
		if svc, exists := r.data.Services[info.Name]; exists {
			if svc.Hosts == nil {
				svc.Hosts = &types.ServiceHosts{
					ByVersion: make(map[string][]string),
				}
			}
			if svc.Hosts.ByVersion == nil {
				svc.Hosts.ByVersion = make(map[string][]string)
			}

			version := info.Version
			if version == "" {
				version = "1"
			}

			// Add IP if not already present
			found := false
			for _, existingIP := range svc.Hosts.ByVersion[version] {
				if existingIP == info.Host {
					found = true
					break
				}
			}
			if !found {
				svc.Hosts.ByVersion[version] = append(svc.Hosts.ByVersion[version], info.Host)
			}

			// Update latest version
			if svc.Hosts.Latest == "" || version > svc.Hosts.Latest {
				svc.Hosts.Latest = version
			}
		}
	}

	return nil
}

// GetHost returns a service host
// This matches Node.js behavior where hosts are stored in registry.services[name].hosts[version]
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

	// Check if service has hosts (merged from hosts collection)
	if svc.Hosts != nil && svc.Hosts.ByVersion != nil {
		requestedVersion := version

		// If version is "1" (default) or empty, try to use latest version first
		if version == "" || version == "1" {
			if svc.Hosts.Latest != "" {
				version = svc.Hosts.Latest
			}
		}

		// Get hosts for this version
		if hosts, ok := svc.Hosts.ByVersion[version]; ok && len(hosts) > 0 {
			return &types.HostInfo{
				Name:    serviceName,
				Version: version,
				Host:    hosts[0],
				Port:    svc.Port,
			}, nil
		}

		// If requested version not found and we didn't try latest yet, try latest
		if requestedVersion != "" && requestedVersion != svc.Hosts.Latest && svc.Hosts.Latest != "" {
			if hosts, ok := svc.Hosts.ByVersion[svc.Hosts.Latest]; ok && len(hosts) > 0 {
				return &types.HostInfo{
					Name:    serviceName,
					Version: svc.Hosts.Latest,
					Host:    hosts[0],
					Port:    svc.Port,
				}, nil
			}
		}
	}

	// Fall back to service config host (if set)
	if svc.Host != "" {
		return &types.HostInfo{
			Name:    serviceName,
			Version: version,
			Host:    svc.Host,
			Port:    svc.Port,
		}, nil
	}

	return nil, fmt.Errorf("no hosts found for service: %s", serviceName)
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

// mergeHostsIntoServices merges host records into service configs
// This matches how Node.js stores hosts: registry.services[name].hosts[version] = [ip1, ip2, ...]
func (r *MongoRegistry) mergeHostsIntoServices(data *types.RegistryData) {
	if data.Hosts == nil {
		return
	}

	for serviceName, hostRecords := range data.Hosts {
		svc, exists := data.Services[serviceName]
		if !exists {
			continue
		}

		// Initialize hosts struct if nil
		if svc.Hosts == nil {
			svc.Hosts = &types.ServiceHosts{
				ByVersion: make(map[string][]string),
			}
		}
		if svc.Hosts.ByVersion == nil {
			svc.Hosts.ByVersion = make(map[string][]string)
		}

		for _, host := range hostRecords {
			version := host.Version
			if version == "" {
				version = "1"
			}

			// Initialize version array if needed
			if svc.Hosts.ByVersion[version] == nil {
				svc.Hosts.ByVersion[version] = []string{}
			}

			// Add IP if not already present
			ip := host.IP
			found := false
			for _, existingIP := range svc.Hosts.ByVersion[version] {
				if existingIP == ip {
					found = true
					break
				}
			}
			if !found {
				svc.Hosts.ByVersion[version] = append(svc.Hosts.ByVersion[version], ip)
			}

			// Track latest version
			if svc.Hosts.Latest == "" || version > svc.Hosts.Latest {
				svc.Hosts.Latest = version
			}

			// Also update port from host record if available (for non-sensitive envs)
			// Node.js does this for non-dashboard environments
			if host.Port > 0 && !isSensitiveEnv(r.env) {
				svc.Port = host.Port
			}
		}
	}
}

// isSensitiveEnv checks if the environment is a sensitive one (like dashboard)
// where port from hosts collection should not override service port
func isSensitiveEnv(env string) bool {
	sensitiveEnvs := []string{"dashboard", "portal"}
	envLower := strings.ToLower(env)
	for _, sensitive := range sensitiveEnvs {
		if envLower == sensitive {
			return true
		}
	}
	return false
}
