package provision

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/security"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// Provision interface for tenant and key management
type Provision interface {
	// LoadProvision loads all provision data
	LoadProvision() error

	// SetKeyConfig sets the key encryption configuration
	SetKeyConfig(algorithm, password string)

	// GetTenant returns tenant by code
	GetTenant(code string) (*types.Tenant, error)

	// GetKey returns key information (accepts external key, decrypts internally)
	GetKey(extKey string) (*types.Key, error)

	// GetProduct returns product by code
	GetProduct(code string) (*types.Product, error)

	// GetPackage returns package by product and package code
	GetPackage(productCode, packageCode string) (*types.Package, error)

	// OAuthModel returns the OAuth model for token management
	OAuthModel() OAuthModel
}

// OAuthModel interface for OAuth operations
type OAuthModel interface {
	GetAccessToken(token string) (*types.AccessToken, error)
	SaveAccessToken(token *types.AccessToken) error
	GetRefreshToken(token string) (*types.RefreshToken, error)
	SaveRefreshToken(token *types.RefreshToken) error
}

// MongoProvision implements Provision using MongoDB
type MongoProvision struct {
	client *mongo.Client
	logger *zap.Logger
	env    string // environment name (e.g., "dashboard")

	mu        sync.RWMutex
	tenants   map[string]*types.Tenant
	products  map[string]*types.Product
	keys      map[string]*types.Key      // keyed by internal key
	keyConfig security.KeyConfig         // key encryption config

	oauthModel *mongoOAuthModel
}

// NewProvision creates a new MongoDB-backed provision
func NewProvision(client *mongo.Client, logger *zap.Logger, env string) *MongoProvision {
	return &MongoProvision{
		client:     client,
		logger:     logger,
		env:        strings.ToLower(env),
		tenants:    make(map[string]*types.Tenant),
		products:   make(map[string]*types.Product),
		keys:       make(map[string]*types.Key),
		oauthModel: &mongoOAuthModel{client: client},
	}
}

// LoadProvision loads all provision data from MongoDB
func (p *MongoProvision) LoadProvision() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Load tenants
	tenantsColl := p.client.Database().Collection("tenants")
	cursor, err := tenantsColl.Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to load tenants: %w", err)
	}
	defer cursor.Close(ctx)

	p.tenants = make(map[string]*types.Tenant)
	p.keys = make(map[string]*types.Key)

	for cursor.Next(ctx) {
		var tenant types.Tenant
		if err := cursor.Decode(&tenant); err != nil {
			p.logger.Warn("failed to decode tenant", zap.Error(err))
			continue
		}
		p.tenants[tenant.Code] = &tenant

		// Index keys by INTERNAL key (key.Key, not extKey.ExtKey)
		// External keys are encrypted versions of internal keys
		for _, app := range tenant.Applications {
			for _, key := range app.Keys {
				// Store by internal key
				internalKey := key.Key
				if internalKey == "" {
					continue
				}

				// Extract environment-specific config (like Node.js does)
				// MongoDB stores: config.dashboard = { commonFields: {...}, oauth: {...}, urac: {...} }
				// We need: config = { commonFields: {...}, oauth: {...}, urac: {...} }
				var envConfig map[string]interface{}
				if key.Config != nil {
					// BSON decodes to primitive.M which is map[string]interface{}
					// But we need to handle both primitive.M and map[string]interface{}
					configMap := toMapStringInterface(key.Config)
					if configMap != nil {
						if envSpecific, ok := configMap[p.env]; ok {
							envConfig = toMapStringInterface(envSpecific)
						}
					}
					if envConfig == nil {
						p.logger.Debug("key config extraction failed",
							zap.String("key", internalKey),
							zap.String("env", p.env),
							zap.Any("configType", fmt.Sprintf("%T", key.Config)),
						)
					}
				}
				if envConfig == nil {
					envConfig = make(map[string]interface{})
				}

				// Create key info with all external keys
				keyInfo := &types.Key{
					InternalKey:  internalKey,
					TenantCode:   tenant.Code,
					TenantID:     tenant.ID,
					ProductCode:  app.Product,
					PackageCode:  app.Package,
					AppID:        app.AppID,
					AppACL:       toMapStringInterface(app.ACL),
					AppACLAllEnv: nil, // Will be populated if present
					Config:       envConfig,
				}

				// Store external key info for each ext key
				for _, extKey := range key.ExtKeys {
					// If this ext key has specific ACL/config, create a separate entry
					if extKey.ACL != nil || extKey.Config != nil {
						// For now, use the first ext key's ACL and config
						if keyInfo.ACL == nil {
							keyInfo.ACL = extKey.ACL
						}
						if keyInfo.Config == nil && extKey.Config != nil {
							keyInfo.Config = extKey.Config
						}
					}
					// Store the external key reference
					keyInfo.ExtKey = extKey.ExtKey
				}

				p.keys[internalKey] = keyInfo
			}
		}
	}

	// Load products
	productsColl := p.client.Database().Collection("products")
	cursor, err = productsColl.Find(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to load products: %w", err)
	}
	defer cursor.Close(ctx)

	p.products = make(map[string]*types.Product)
	for cursor.Next(ctx) {
		var product types.Product
		if err := cursor.Decode(&product); err != nil {
			p.logger.Warn("failed to decode product", zap.Error(err))
			continue
		}
		p.products[product.Code] = &product
	}

	p.logger.Info("provision loaded",
		zap.Int("tenants", len(p.tenants)),
		zap.Int("products", len(p.products)),
		zap.Int("keys", len(p.keys)),
	)

	return nil
}

// GetTenant returns tenant by code
func (p *MongoProvision) GetTenant(code string) (*types.Tenant, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	tenant, exists := p.tenants[code]
	if !exists {
		return nil, fmt.Errorf("tenant not found: %s", code)
	}

	return tenant, nil
}

// SetKeyConfig sets the key encryption configuration
func (p *MongoProvision) SetKeyConfig(algorithm, password string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.keyConfig = security.KeyConfig{
		Algorithm: algorithm,
		Password:  password,
	}
}

// GetKey returns key information by decrypting the external key
func (p *MongoProvision) GetKey(extKey string) (*types.Key, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// Decrypt the external key to get the internal key
	keyInfo, err := security.DecryptKey(extKey, p.keyConfig)
	if err != nil {
		p.logger.Debug("failed to decrypt key", zap.Error(err))
		return nil, fmt.Errorf("invalid key format")
	}

	// Look up by internal key
	key, exists := p.keys[keyInfo.Key]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}

	// Attach the external key that was used
	key.ExtKey = extKey

	return key, nil
}

// GetProduct returns product by code
func (p *MongoProvision) GetProduct(code string) (*types.Product, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	product, exists := p.products[code]
	if !exists {
		return nil, fmt.Errorf("product not found: %s", code)
	}

	return product, nil
}

// GetPackage returns package by product and package code
func (p *MongoProvision) GetPackage(productCode, packageCode string) (*types.Package, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	product, exists := p.products[productCode]
	if !exists {
		return nil, fmt.Errorf("product not found: %s", productCode)
	}

	for _, pkg := range product.Packages {
		if pkg.Code == packageCode {
			return pkg, nil
		}
	}

	return nil, fmt.Errorf("package not found: %s/%s", productCode, packageCode)
}

// OAuthModel returns the OAuth model
func (p *MongoProvision) OAuthModel() OAuthModel {
	return p.oauthModel
}

// mongoOAuthModel implements OAuthModel using MongoDB
type mongoOAuthModel struct {
	client *mongo.Client
}

func (m *mongoOAuthModel) GetAccessToken(token string) (*types.AccessToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// OAuth tokens are stored in the "oauth" database, "oauth_token" collection
	coll := m.client.UseDatabase("oauth").Collection("oauth_token")
	var t types.AccessToken
	err := coll.FindOne(ctx, bson.M{"token": token}).Decode(&t)
	return &t, err
}

func (m *mongoOAuthModel) SaveAccessToken(token *types.AccessToken) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := m.client.Database().Collection("oauth_tokens")
	_, err := coll.InsertOne(ctx, token)
	return err
}

func (m *mongoOAuthModel) GetRefreshToken(token string) (*types.RefreshToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := m.client.Database().Collection("oauth_refresh_tokens")
	var t types.RefreshToken
	err := coll.FindOne(ctx, bson.M{"token": token}).Decode(&t)
	return &t, err
}

func (m *mongoOAuthModel) SaveRefreshToken(token *types.RefreshToken) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	coll := m.client.Database().Collection("oauth_refresh_tokens")
	_, err := coll.InsertOne(ctx, token)
	return err
}

// toMapStringInterface converts interface{} to map[string]interface{}
// Handles map[string]interface{}, bson.M (primitive.M), and bson.D (primitive.D)
func toMapStringInterface(v interface{}) map[string]interface{} {
	return convertBSONToMap(v)
}

// convertBSONToMap recursively converts BSON types to map[string]interface{}
func convertBSONToMap(v interface{}) map[string]interface{} {
	if v == nil {
		return nil
	}
	// Direct map[string]interface{} - recursively convert values
	if m, ok := v.(map[string]interface{}); ok {
		result := make(map[string]interface{})
		for k, val := range m {
			result[k] = convertBSONValue(val)
		}
		return result
	}
	// BSON primitive.M (which is bson.M) - recursively convert values
	if m, ok := v.(bson.M); ok {
		result := make(map[string]interface{})
		for k, val := range m {
			result[k] = convertBSONValue(val)
		}
		return result
	}
	// BSON primitive.D (ordered document - slice of {Key, Value})
	if d, ok := v.(bson.D); ok {
		result := make(map[string]interface{})
		for _, elem := range d {
			result[elem.Key] = convertBSONValue(elem.Value)
		}
		return result
	}
	return nil
}

// convertBSONValue recursively converts a BSON value to native Go types
func convertBSONValue(v interface{}) interface{} {
	if v == nil {
		return nil
	}
	// Convert bson.D to map
	if d, ok := v.(bson.D); ok {
		result := make(map[string]interface{})
		for _, elem := range d {
			result[elem.Key] = convertBSONValue(elem.Value)
		}
		return result
	}
	// Convert bson.M to map
	if m, ok := v.(bson.M); ok {
		result := make(map[string]interface{})
		for k, val := range m {
			result[k] = convertBSONValue(val)
		}
		return result
	}
	// Convert bson.A (array) to slice
	if a, ok := v.(bson.A); ok {
		result := make([]interface{}, len(a))
		for i, val := range a {
			result[i] = convertBSONValue(val)
		}
		return result
	}
	// Return primitives as-is
	return v
}
