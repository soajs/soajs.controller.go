package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/internal/dbconfig"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// Collection names
const (
	CollEnvironment    = "environment"
	CollInfra          = "infra"
	CollHosts          = "hosts"
	CollControllers    = "controllers"
	CollResources      = "resources"
	CollCustomRegistry = "custom_registry"
	CollMarketplace    = "marketplace"
)

// MongoRegistry loads the registry from the provision database the profile names.
type MongoRegistry struct {
	db      *mongo.Database
	profile *dbconfig.Profile
	logger  *zap.Logger
	isHA    bool

	mu   sync.RWMutex
	env  string
	data *types.RegistryData

	othersMu sync.Mutex
	others   map[string]*types.RegistryData

	reloadMu sync.Mutex
	stop     chan struct{}
	stopOnce sync.Once
}

// NewMongoRegistry creates a registry reading from db, the provision database of profile.
func NewMongoRegistry(db *mongo.Database, profile *dbconfig.Profile, logger *zap.Logger) *MongoRegistry {
	return &MongoRegistry{
		db:      db,
		profile: profile,
		logger:  logger,
		isHA:    os.Getenv("SOAJS_DEPLOY_HA") != "",
		others:  map[string]*types.RegistryData{},
		stop:    make(chan struct{}),
	}
}

// Load loads the registry of env and starts the automatic reload.
func (r *MongoRegistry) Load(env string) error {
	env = strings.ToLower(env)
	data, err := r.build(env)
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.env = env
	r.data = data
	r.mu.Unlock()
	go r.autoReload()
	return nil
}

// Reload loads the registry again; on error the previous one stays in use.
func (r *MongoRegistry) Reload() error {
	r.reloadMu.Lock()
	defer r.reloadMu.Unlock()
	data, err := r.build(r.GetEnvironment())
	if err != nil {
		return err
	}
	r.mu.Lock()
	r.data = data
	r.mu.Unlock()
	return nil
}

// autoReload reloads the registry every awareness.autoRelaodRegistry ms, never two at a time
// (Node.js modules/registry/index.js:498-523).
func (r *MongoRegistry) autoReload() {
	for {
		interval := 0
		if d := r.Get(); d != nil && d.ServiceConfig != nil {
			interval = d.ServiceConfig.Awareness.AutoRelaodRegistry
		}
		if interval <= 0 {
			return
		}
		select {
		case <-r.stop:
			return
		case <-time.After(time.Duration(interval) * time.Millisecond):
		}
		if err := r.Reload(); err != nil {
			r.logger.Error("automatic registry reload failed, the previous registry stays in use", zap.Error(err))
		}
	}
}

// Close stops the automatic reload.
func (r *MongoRegistry) Close() {
	r.stopOnce.Do(func() { close(r.stop) })
}

// Get returns the current registry.
func (r *MongoRegistry) Get() *types.RegistryData {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data
}

// GetEnvironment returns the gateway's environment code, lowercase.
func (r *MongoRegistry) GetEnvironment() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.env
}

// LoadByEnv returns the registry of another environment, from the cache unless reload.
func (r *MongoRegistry) LoadByEnv(env string, reload bool) (*types.RegistryData, error) {
	env = strings.ToLower(env)
	if env == r.GetEnvironment() && !reload {
		return r.Get(), nil
	}
	r.othersMu.Lock()
	defer r.othersMu.Unlock()
	if d, ok := r.others[env]; ok && !reload {
		return d, nil
	}
	d, err := r.build(env)
	if err != nil {
		return nil, err
	}
	r.others[env] = d
	return d, nil
}

// build loads and builds the registry of env (Node.js getRegistry, buildRegistry and the gateway
// part of buildSpecificRegistry).
func (r *MongoRegistry) build(env string) (*types.RegistryData, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schemas, err := dataschema.Embedded()
	if err != nil {
		return nil, err
	}
	envUpper := upper(env)
	timeLoaded := time.Now().UnixMilli()

	var e envDoc
	raw, err := r.db.Collection(CollEnvironment).FindOne(ctx, bson.M{"code": envUpper}).Raw()
	if err != nil {
		return nil, fmt.Errorf("environment %s: %w", envUpper, err)
	}
	if !r.read(schemas, CollEnvironment, raw, &e) {
		return nil, fmt.Errorf("environment %s cannot be read: see /schemaProblems", envUpper)
	}

	reg := &types.RegistryData{
		TimeLoaded:     timeLoaded,
		Name:           env,
		Environment:    env,
		Domain:         e.Domain,
		SitePrefix:     e.SitePrefix,
		APIPrefix:      e.APIPrefix,
		Protocol:       e.Protocol,
		Port:           e.Port,
		ServiceConfig:  &e.Services.Config,
		Deployer:       e.Deployer,
		CustomRegistry: map[string]interface{}{},
		Resources:      map[string]map[string]map[string]interface{}{},
		Services:       map[string]*types.ServiceConfig{gatewayName: controllerEntry(&e)},
		Daemons:        map[string]*types.DaemonConfig{},
	}

	if err := r.loadInfra(ctx, schemas, reg); err != nil {
		return nil, err
	}

	criteria := bson.M{"$or": []bson.M{
		{"created": envUpper, "plugged": true},
		{"plugged": true, "shared": true},
	}}
	if err := r.each(ctx, schemas, CollResources, criteria, func(doc map[string]interface{}) {
		if !available(doc, envUpper) {
			return
		}
		t, _ := doc["type"].(string)
		name, _ := doc["name"].(string)
		if reg.Resources[t] == nil {
			reg.Resources[t] = map[string]map[string]interface{}{}
		}
		reg.Resources[t][name] = doc
	}); err != nil {
		return nil, err
	}
	if err := r.each(ctx, schemas, CollCustomRegistry, criteria, func(doc map[string]interface{}) {
		if !available(doc, envUpper) {
			return
		}
		name, _ := doc["name"].(string)
		reg.CustomRegistry[name] = doc
	}); err != nil {
		return nil, err
	}

	core, meta, err := buildDatabases(&e, reg.Resources["cluster"], env, timeLoaded)
	if err != nil {
		return nil, fmt.Errorf("environment %s: %w", envUpper, err)
	}
	reg.CoreDB = map[string]*dbconfig.DB{"provision": r.profile.ProvisionDB(env, timeLoaded)}
	for name, db := range core {
		reg.CoreDB[name] = db
	}
	reg.TenantMetaDB = meta
	reg.SessionDB = map[string]interface{}{
		"prefix":           e.Dbs.Config.Prefix,
		"registryLocation": map[string]interface{}{"l1": "coreDB", "l2": "session", "env": env, "timeLoaded": timeLoaded},
		"servers":          nil, "credentials": nil, "URLParam": nil, "extraParam": nil,
	}

	if err := r.eachTyped(ctx, schemas, CollMarketplace, bson.M{"type": "service"}, func(item *marketItem) {
		addService(reg.Services, item)
	}); err != nil {
		return nil, err
	}
	if err := r.eachTyped(ctx, schemas, CollMarketplace, bson.M{"type": "mdaemon"}, func(item *marketItem) {
		addDaemon(reg.Daemons, item)
	}); err != nil {
		return nil, err
	}

	if !r.isHA {
		var hosts []*types.HostRecord
		if err := r.eachRaw(ctx, schemas, CollHosts, bson.M{"env": env}, func(raw bson.Raw) {
			var h types.HostRecord
			if r.read(schemas, CollHosts, raw, &h) {
				hosts = append(hosts, &h)
			}
		}); err != nil {
			return nil, err
		}
		applyHosts(reg, hosts)
	}
	return reg, nil
}

// loadInfra copies the infra account's configuration into the kubernetes deployer. A missing or
// invalid account is logged; only HA awareness needs it.
func (r *MongoRegistry) loadInfra(ctx context.Context, schemas map[string]*dataschema.Collection, reg *types.RegistryData) error {
	if reg.Deployer["type"] != "container" {
		return nil
	}
	container, _ := reg.Deployer["container"].(map[string]interface{})
	k8s, _ := container["kubernetes"].(map[string]interface{})
	id, _ := k8s["id"].(string)
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return fmt.Errorf("deployer.container.kubernetes.id is not an ObjectId")
	}
	raw, err := r.db.Collection(CollInfra).FindOne(ctx, bson.M{"_id": oid}).Raw()
	if err == mongo.ErrNoDocuments {
		r.logger.Error("the deployer's infra account does not exist", zap.String("id", id))
		return nil
	}
	if err != nil {
		return err
	}
	var infra map[string]interface{}
	if !r.read(schemas, CollInfra, raw, &infra) {
		return nil
	}
	k8s["configuration"] = infra["configuration"]
	return nil
}

// each decodes every document matching filter as a plain map. A document that does not match the
// data schema is logged by _id and skipped.
func (r *MongoRegistry) each(ctx context.Context, schemas map[string]*dataschema.Collection, coll string, filter interface{}, fn func(map[string]interface{})) error {
	return r.eachRaw(ctx, schemas, coll, filter, func(raw bson.Raw) {
		var doc map[string]interface{}
		if !r.read(schemas, coll, raw, &doc) {
			return
		}
		fn(doc)
	})
}

// eachTyped is each for marketplace items.
func (r *MongoRegistry) eachTyped(ctx context.Context, schemas map[string]*dataschema.Collection, coll string, filter interface{}, fn func(*marketItem)) error {
	return r.eachRaw(ctx, schemas, coll, filter, func(raw bson.Raw) {
		var item marketItem
		if !r.read(schemas, coll, raw, &item) {
			return
		}
		fn(&item)
	})
}

func (r *MongoRegistry) eachRaw(ctx context.Context, schemas map[string]*dataschema.Collection, coll string, filter interface{}, fn func(bson.Raw)) error {
	if schemas[coll] == nil {
		return fmt.Errorf("no data schema for %s", coll)
	}
	cursor, err := r.db.Collection(coll).Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("%s: %w", coll, err)
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		fn(cursor.Current)
	}
	return cursor.Err()
}

// policies are how each registry collection is read when a document does not match its schema
// (docs/data-schema/imperfect-data.md).
var policies = map[string]dataschema.Policy{
	CollEnvironment:    dataschema.Environment,
	CollCustomRegistry: dataschema.CustomRegistry,
	CollMarketplace:    dataschema.Marketplace,
}

// read reads a document through its collection's policy and reports what it found
// (/schemaProblems).
func (r *MongoRegistry) read(schemas map[string]*dataschema.Collection, coll string, raw bson.Raw, out interface{}) bool {
	policy := policies[coll]
	if policy == nil {
		policy = dataschema.WholeDocument
	}
	findings, ok, err := schemas[coll].Read(raw, policy, out)
	if err != nil {
		r.logger.Error("unable to read a document", zap.String("collection", coll), zap.Error(err))
		return false
	}
	id := ""
	if v, e := raw.LookupErr("_id"); e == nil {
		if oid, isOID := v.ObjectIDOK(); isOID {
			id = oid.Hex()
		}
	}
	dataschema.Problems.Report(r.logger, coll, id, findings)
	return ok
}

// OtherEnvControllerHosts reads the gateway's hosts of the other environments
// (modules/registry/mongo.js loadOtherEnvHosts): name matching the gateway's name as a
// case-insensitive pattern, env not the gateway's.
func (r *MongoRegistry) OtherEnvControllerHosts(gatewayName string) ([]types.HostRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	schemas, err := dataschema.Embedded()
	if err != nil {
		return nil, err
	}
	filter := bson.M{"name": primitive.Regex{Pattern: gatewayName, Options: "i"}, "env": bson.M{"$ne": r.GetEnvironment()}}
	var hosts []types.HostRecord
	err = r.each(ctx, schemas, CollHosts, filter, func(doc map[string]interface{}) {
		var h types.HostRecord
		if remarshal(doc, &h) == nil {
			hosts = append(hosts, h)
		}
	})
	return hosts, err
}

// UpsertEnvController writes the controllers document of the environment and IP
// (modules/registry/mongo.js addUpdateEnvControllers): {$set: {data, ts}}, upserted.
func (r *MongoRegistry) UpsertEnvController(env, ip string, ts int64, data map[string]interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	filter := bson.M{"env": strings.ToLower(env), "ip": ip}
	_, err := r.db.Collection(CollControllers).UpdateOne(ctx, filter, bson.M{"$set": bson.M{"data": data, "ts": ts}}, options.Update().SetUpsert(true))
	return err
}

// RegisterHost upserts a host as Node.js does (modules/registry/mongo.js:262-286); the registry in
// use takes it at its next load, as in Node.js.
func (r *MongoRegistry) RegisterHost(h types.HostRecord) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.Env = strings.ToLower(h.Env)
	filter := bson.M{"env": h.Env, "name": h.Name, "version": h.Version}
	if h.ServiceHATask != "" {
		filter["serviceHATask"] = h.ServiceHATask
	} else {
		filter["ip"] = h.IP
		filter["hostname"] = h.Hostname
	}
	set := bson.M{"env": h.Env, "name": h.Name, "ip": h.IP, "port": h.Port, "hostname": h.Hostname, "version": h.Version}
	if h.Type != "" {
		set["type"] = h.Type
	}
	if h.ServiceHATask != "" {
		set["serviceHATask"] = h.ServiceHATask
	}
	_, err := r.db.Collection(CollHosts).UpdateOne(ctx, filter, bson.M{"$set": set}, options.Update().SetUpsert(true))
	return err
}

// GetHost returns the first host of a service version, or of its latest version when version is
// empty. Health and round-robin come with the awareness rebuild.
func (r *MongoRegistry) GetHost(serviceName, version string) (*types.HostInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.data == nil {
		return nil, fmt.Errorf("registry not loaded")
	}
	svc, ok := r.data.Services[serviceName]
	if !ok {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}
	if svc.Hosts == nil {
		return nil, fmt.Errorf("no hosts found for service: %s", serviceName)
	}
	if version == "" {
		version = svc.Hosts.Latest
	}
	ips := svc.Hosts.ByVersion[version]
	if len(ips) == 0 {
		return nil, fmt.Errorf("no hosts found for service %s version %s", serviceName, version)
	}
	return &types.HostInfo{Name: serviceName, Version: version, Host: ips[0], Port: svc.Port}, nil
}

// remarshal copies a plain JSON value into a typed one.
func remarshal(in, out interface{}) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
