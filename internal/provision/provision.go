package provision

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/internal/soajserr"
	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/security"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// Provision resolves keys, packages and tenants, as the Node.js soajs.provision does: from a cache
// filled at load time, refreshed from the database on a miss or when an entry's _TTL has passed.
type Provision interface {
	// LoadProvision loads every tenant and package into the cache.
	LoadProvision() error

	// SetKeyConfig sets the extKey cipher (registry serviceConfig.key).
	SetKeyConfig(algorithm, password string)

	// GetExternalKeyData resolves an external key. It returns nil without an error when the key
	// exists but the extKey is not one of its extKeys, or has expired.
	GetExternalKeyData(extKey string) (*types.Key, error)

	// GetPackageData returns a package by code.
	GetPackageData(code string) (*types.Package, error)

	// GetTenantOauth returns a tenant's oauth settings.
	GetTenantOauth(tenantID string) (*types.TenantOauth, error)

	// GetTenantCode returns a tenant's code.
	GetTenantCode(tenantID string) (string, error)

	// OAuthModel returns the access token store.
	OAuthModel() OAuthModel
}

// OAuthModel reads access tokens.
type OAuthModel interface {
	// GetAccessToken returns an access token, or nil when there is none for this environment.
	GetAccessToken(token string) (*types.AccessToken, error)
}

// tenantDoc is a tenants document (docs/data-schema/tenants.md).
type tenantDoc struct {
	ID           string                 `json:"_id"`
	Code         string                 `json:"code"`
	Name         string                 `json:"name"`
	Type         string                 `json:"type"`
	Locked       bool                   `json:"locked"`
	Profile      map[string]interface{} `json:"profile"`
	Tenant       *types.TenantRef       `json:"tenant"`
	Oauth        types.TenantOauth      `json:"oauth"`
	Applications []struct {
		AppID   string `json:"appId"`
		Product string `json:"product"`
		Package string `json:"package"`
		TTL     int64  `json:"_TTL"`
		Keys    []struct {
			Key     string                 `json:"key"`
			Config  map[string]interface{} `json:"config"`
			ExtKeys []extKeyDoc            `json:"extKeys"`
		} `json:"keys"`
	} `json:"applications"`
}

type extKeyDoc struct {
	ExtKey  string                 `json:"extKey"`
	Env     string                 `json:"env"`
	Device  map[string]interface{} `json:"device"`
	Geo     map[string]interface{} `json:"geo"`
	ExpDate *int64                 `json:"expDate"`
}

// productDoc is a products document (docs/data-schema/products.md).
type productDoc struct {
	Packages []struct {
		Code string                 `json:"code"`
		TTL  int64                  `json:"_TTL"`
		ACL  map[string]interface{} `json:"acl"`
	} `json:"packages"`
}

type keyEntry struct {
	key     string
	tenant  types.KeyTenant
	app     types.KeyApplication
	extKeys []extKeyDoc
	config  map[string]interface{}
	ttl     int64
	loaded  int64
}

type packageEntry struct {
	pkg    types.Package
	loaded int64
}

// MongoProvision implements Provision on the provision database.
type MongoProvision struct {
	db     *drv.Database
	logger *zap.Logger
	env    string

	mu        sync.RWMutex
	keyConfig security.KeyConfig
	keys      map[string]*keyEntry
	packages  map[string]*packageEntry
	oauths    map[string]*types.TenantOauth
	codes     map[string]string

	oauthModel *mongoOAuthModel
}

// NewProvision creates a provision whose access tokens live in the provision database.
func NewProvision(client *mongo.Client, logger *zap.Logger, env string) *MongoProvision {
	return NewProvisionWithOAuthClient(client, client, logger, env)
}

// NewProvisionWithOAuthClient creates a provision whose access tokens live in oauthClient's
// database: the environment's oauth database.
func NewProvisionWithOAuthClient(client *mongo.Client, oauthClient *mongo.Client, logger *zap.Logger, env string) *MongoProvision {
	envLower := strings.ToLower(env)
	return &MongoProvision{
		db:         client.Database(),
		logger:     logger,
		env:        envLower,
		keys:       map[string]*keyEntry{},
		packages:   map[string]*packageEntry{},
		oauths:     map[string]*types.TenantOauth{},
		codes:      map[string]string{},
		oauthModel: &mongoOAuthModel{db: oauthClient.Database(), env: envLower, logger: logger},
	}
}

// SetKeyConfig sets the extKey cipher.
func (p *MongoProvision) SetKeyConfig(algorithm, password string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.keyConfig = security.KeyConfig{Algorithm: algorithm, Password: password}
}

// LoadProvision loads every package and every tenant (soajs.provision loadProvision).
func (p *MongoProvision) LoadProvision() error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := p.loadPackages(ctx, bson.M{}, ""); err != nil {
		return err
	}
	if err := p.loadTenants(ctx, bson.M{}); err != nil {
		return err
	}
	p.mu.RLock()
	p.logger.Info("provision loaded", zap.Int("keys", len(p.keys)), zap.Int("packages", len(p.packages)), zap.Int("tenants", len(p.codes)))
	p.mu.RUnlock()
	return nil
}

// GetExternalKeyData resolves an external key (soajs.provision getExternalKeyData).
func (p *MongoProvision) GetExternalKeyData(extKey string) (*types.Key, error) {
	if extKey == "" {
		return nil, codeError(200)
	}
	p.mu.RLock()
	keyConfig := p.keyConfig
	p.mu.RUnlock()
	info, err := security.DecryptKey(extKey, keyConfig)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	p.mu.RLock()
	entry := p.keys[info.Key]
	if entry != nil && (entry.ttl == 0 || entry.loaded > now-entry.ttl) {
		if k := keyData(entry, extKey, now); k != nil {
			p.mu.RUnlock()
			return k, nil
		}
	}
	p.mu.RUnlock()

	// As in Node.js, a key that is not cached, has passed its _TTL, or whose extKey is not one of
	// its listed, unexpired extKeys is read again from the database, on every such request: an
	// extKey just added or renewed works at once.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.loadTenants(ctx, bson.M{"applications.keys.key": info.Key}); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	entry = p.keys[info.Key]
	if entry == nil {
		return nil, codeError(153)
	}
	return keyData(entry, extKey, now), nil
}

// keyData is what a cached key gives for one of its extKeys: nothing when the extKey is not one of
// them or has expired (soajs.provision getKeyData). It is a deep copy on every call: a request may
// change it, the cache never sees it.
func keyData(entry *keyEntry, extKey string, now int64) *types.Key {
	for _, e := range entry.extKeys {
		if e.ExtKey != extKey {
			continue
		}
		if e.ExpDate != nil && *e.ExpDate != 0 && *e.ExpDate <= now {
			return nil
		}
		tenant := entry.tenant
		tenant.Profile = copyMap(entry.tenant.Profile)
		if entry.tenant.Main != nil {
			main := *entry.tenant.Main
			tenant.Main = &main
		}
		return &types.Key{
			InternalKey: entry.key,
			ExtKey:      extKey,
			Tenant:      tenant,
			Application: entry.app,
			Device:      copyMap(e.Device),
			Env:         e.Env,
			Geo:         copyMap(e.Geo),
			Config:      copyMap(entry.config),
		}
	}
	return nil
}

// GetPackageData returns a package (soajs.provision getPackageData), a deep copy of the cached one.
func (p *MongoProvision) GetPackageData(code string) (*types.Package, error) {
	if code == "" {
		return nil, codeError(201)
	}
	now := time.Now().UnixMilli()
	p.mu.RLock()
	entry := p.packages[code]
	p.mu.RUnlock()
	if entry == nil || (entry.pkg.TTL != 0 && entry.loaded <= now-entry.pkg.TTL) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := p.loadPackages(ctx, bson.M{"packages.code": code}, code); err != nil {
			return nil, err
		}
		p.mu.RLock()
		entry = p.packages[code]
		p.mu.RUnlock()
		if entry == nil {
			return nil, codeError(201)
		}
	}
	pkg := entry.pkg
	pkg.ACL = copyMap(entry.pkg.ACL)
	pkg.ACLAllEnv = copyMap(entry.pkg.ACLAllEnv)
	return &pkg, nil
}

// GetTenantOauth returns a tenant's oauth settings, cached once read (soajs.provision getTenantOauth).
func (p *MongoProvision) GetTenantOauth(tenantID string) (*types.TenantOauth, error) {
	if tenantID == "" {
		return nil, codeError(205)
	}
	if err := p.ensureTenant(tenantID); err != nil {
		return nil, err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	o := *p.oauths[tenantID]
	return &o, nil
}

// GetTenantCode returns a tenant's code, cached once read (soajs.provision getTenantData).
func (p *MongoProvision) GetTenantCode(tenantID string) (string, error) {
	if tenantID == "" {
		return "", codeError(205)
	}
	if err := p.ensureTenant(tenantID); err != nil {
		return "", err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.codes[tenantID], nil
}

func (p *MongoProvision) ensureTenant(tenantID string) error {
	p.mu.RLock()
	_, ok := p.codes[tenantID]
	p.mu.RUnlock()
	if ok {
		return nil
	}
	oid, err := primitive.ObjectIDFromHex(tenantID)
	if err != nil {
		return codeError(206)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.loadTenants(ctx, bson.M{"_id": oid}); err != nil {
		return err
	}
	p.mu.RLock()
	_, ok = p.codes[tenantID]
	p.mu.RUnlock()
	if !ok {
		return codeError(206)
	}
	return nil
}

// loadTenants reads tenants and caches their keys, oauth and code. A tenant that does not match
// the data schema is logged by _id and skipped.
func (p *MongoProvision) loadTenants(ctx context.Context, filter bson.M) error {
	schema, err := schemaFor("tenants")
	if err != nil {
		return err
	}
	cursor, err := p.db.Collection("tenants").Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("tenants: %w", err)
	}
	defer cursor.Close(ctx)
	now := time.Now().UnixMilli()
	for cursor.Next(ctx) {
		var t tenantDoc
		if problems, err := schema.Decode(cursor.Current, &t); err != nil || problems != nil {
			p.skip("tenants", cursor.Current, problems, err)
			continue
		}
		p.ingestTenant(&t, now)
	}
	return cursor.Err()
}

func (p *MongoProvision) ingestTenant(t *tenantDoc, now int64) {
	tenant := types.KeyTenant{
		Name:    t.Name,
		ID:      t.ID,
		Code:    t.Code,
		Locked:  t.Locked,
		Type:    t.Type,
		Profile: t.Profile,
	}
	if tenant.Profile == nil {
		tenant.Profile = map[string]interface{}{}
	}
	if t.Type == "client" && t.Tenant != nil {
		tenant.Main = t.Tenant
	}
	oauth := t.Oauth
	p.mu.Lock()
	defer p.mu.Unlock()
	p.codes[t.ID] = t.Code
	p.oauths[t.ID] = &oauth
	for _, app := range t.Applications {
		for _, k := range app.Keys {
			config, _ := k.Config[p.env].(map[string]interface{})
			if config == nil {
				config = map[string]interface{}{}
			}
			p.keys[k.Key] = &keyEntry{
				key:     k.Key,
				tenant:  tenant,
				app:     types.KeyApplication{Product: app.Product, Package: app.Package, AppID: app.AppID},
				extKeys: k.ExtKeys,
				config:  config,
				ttl:     app.TTL,
				loaded:  now,
			}
		}
	}
}

// loadPackages reads products and caches their packages, or only the package code when given.
// A package's ACL is its acl for the gateway's environment (granular only).
func (p *MongoProvision) loadPackages(ctx context.Context, filter bson.M, code string) error {
	schema, err := schemaFor("products")
	if err != nil {
		return err
	}
	cursor, err := p.db.Collection("products").Find(ctx, filter)
	if err != nil {
		return fmt.Errorf("products: %w", err)
	}
	defer cursor.Close(ctx)
	now := time.Now().UnixMilli()
	for cursor.Next(ctx) {
		var prod productDoc
		if problems, err := schema.Decode(cursor.Current, &prod); err != nil || problems != nil {
			p.skip("products", cursor.Current, problems, err)
			continue
		}
		entries := packageEntries(&prod, p.env, code, now)
		p.mu.Lock()
		for c, e := range entries {
			p.packages[c] = e
		}
		p.mu.Unlock()
	}
	return cursor.Err()
}

// packageEntries are a product's packages, or only code when given. A package's ACL is its acl
// for env (granular only); acl_all_env is the whole acl.
func packageEntries(prod *productDoc, env, code string, now int64) map[string]*packageEntry {
	entries := map[string]*packageEntry{}
	for _, pk := range prod.Packages {
		if code != "" && pk.Code != code {
			continue
		}
		acl, _ := pk.ACL[env].(map[string]interface{})
		entries[pk.Code] = &packageEntry{
			pkg:    types.Package{Code: pk.Code, ACL: acl, ACLAllEnv: pk.ACL, TTL: pk.TTL},
			loaded: now,
		}
	}
	return entries
}

func (p *MongoProvision) skip(coll string, raw bson.Raw, problems []dataschema.Problem, err error) {
	logSkipped(p.logger, coll, raw, problems, err)
}

// OAuthModel returns the access token store.
func (p *MongoProvision) OAuthModel() OAuthModel {
	return p.oauthModel
}

// mongoOAuthModel reads oauth_token in the environment's oauth database, or in provision when the
// environment has none; the environment's dbs decide, there is no lookup in one and then the other.
type mongoOAuthModel struct {
	db     *drv.Database
	env    string
	logger *zap.Logger
}

// GetAccessToken returns the access token, when it was issued in this environment or in a
// sensitive one (soajs.core provision getAccessToken).
func (m *mongoOAuthModel) GetAccessToken(token string) (*types.AccessToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := m.db.Collection("oauth_token").FindOne(ctx, bson.M{"token": token, "type": "accessToken"}).Raw()
	if err == drv.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	schema, err := schemaFor("oauth_token")
	if err != nil {
		return nil, err
	}
	var t types.AccessToken
	if problems, err := schema.Decode(raw, &t); err != nil || problems != nil {
		logSkipped(m.logger, "oauth_token", raw, problems, err)
		return nil, nil
	}
	if t.Env != m.env && !registry.IsSensitiveEnv(t.Env) {
		return nil, nil
	}
	return &t, nil
}

func schemaFor(coll string) (*dataschema.Collection, error) {
	schemas, err := dataschema.Embedded()
	if err != nil {
		return nil, err
	}
	s := schemas[coll]
	if s == nil {
		return nil, fmt.Errorf("no data schema for %s", coll)
	}
	return s, nil
}

// logSkipped logs a document that does not match the data schema: its _id and where, never its
// values.
func logSkipped(logger *zap.Logger, coll string, raw bson.Raw, problems []dataschema.Problem, err error) {
	id := ""
	if v, e := raw.LookupErr("_id"); e == nil {
		if oid, ok := v.ObjectIDOK(); ok {
			id = oid.Hex()
		}
	}
	logger.Error("document skipped, it does not match the data schema",
		zap.String("collection", coll), zap.String("_id", id), zap.Any("problems", problems), zap.Error(err))
}

// copyMap deep-copies a decoded JSON object: maps and arrays are copied, other values are
// immutable.
func copyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = copyValue(v)
	}
	return out
}

func copyValue(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		return copyMap(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, e := range t {
			out[i] = copyValue(e)
		}
		return out
	}
	return v
}

// CodeError is a provision error with a SOAJS error code.
type CodeError struct {
	Code int
	Msg  string
}

func (e *CodeError) Error() string { return e.Msg }

func codeError(code int) error {
	return &CodeError{Code: code, Msg: soajserr.Get(code).Msg}
}
