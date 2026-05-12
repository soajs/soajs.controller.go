package provision

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

// Provision interface for tenant and key management
type Provision interface {
	// LoadProvision loads all provision data
	LoadProvision() error

	// GetTenant returns tenant by code
	GetTenant(code string) (*types.Tenant, error)

	// GetKey returns key information
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

	mu       sync.RWMutex
	tenants  map[string]*types.Tenant
	products map[string]*types.Product
	keys     map[string]*types.Key

	oauthModel *mongoOAuthModel
}

// NewProvision creates a new MongoDB-backed provision
func NewProvision(client *mongo.Client, logger *zap.Logger) *MongoProvision {
	return &MongoProvision{
		client:     client,
		logger:     logger,
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

		// Index keys
		for _, app := range tenant.Applications {
			for _, key := range app.Keys {
				for _, extKey := range key.ExtKeys {
					p.keys[extKey.ExtKey] = &types.Key{
						ExtKey:      extKey.ExtKey,
						TenantCode:  tenant.Code,
						TenantID:    tenant.ID,
						ProductCode: app.Product,
						PackageCode: app.Package,
						ACL:         extKey.ACL,
						Config:      extKey.Config,
					}
				}
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

// GetKey returns key information
func (p *MongoProvision) GetKey(extKey string) (*types.Key, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	key, exists := p.keys[extKey]
	if !exists {
		return nil, fmt.Errorf("key not found")
	}

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

	coll := m.client.Database().Collection("oauth_tokens")
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
