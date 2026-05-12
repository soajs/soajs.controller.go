package urac

import (
	"context"

	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"github.com/soajs/soajs.controller.go/pkg/urac/acl"
	"github.com/soajs/soajs.controller.go/pkg/urac/hasher"
	"github.com/soajs/soajs.controller.go/pkg/urac/model"
	"go.uber.org/zap"
)

// Driver provides URAC functionality
type Driver struct {
	mongo      *mongo.Client
	logger     *zap.Logger
	user       *UserService
	group      *GroupService
	hashConfig hasher.Config
	env        string
}

// Config holds driver configuration
type Config struct {
	// HashIterations for password hashing (default: 12)
	HashIterations int
	// OptionalAlgorithm for pre-hashing passwords (e.g., "sha256")
	OptionalAlgorithm string
	// Environment code for ACL resolution
	Environment string
}

// DefaultConfig returns default driver configuration
func DefaultConfig() Config {
	return Config{
		HashIterations:    12,
		OptionalAlgorithm: "",
		Environment:       "dev",
	}
}

// NewDriver creates a new URAC driver
func NewDriver(mongoClient *mongo.Client, logger *zap.Logger) *Driver {
	return NewDriverWithConfig(mongoClient, logger, DefaultConfig())
}

// NewDriverWithConfig creates a new URAC driver with custom config
func NewDriverWithConfig(mongoClient *mongo.Client, logger *zap.Logger, cfg Config) *Driver {
	d := &Driver{
		mongo:  mongoClient,
		logger: logger,
		env:    cfg.Environment,
		hashConfig: hasher.Config{
			HashIterations:    cfg.HashIterations,
			OptionalAlgorithm: cfg.OptionalAlgorithm,
		},
	}
	d.user = &UserService{driver: d}
	d.group = &GroupService{driver: d}
	return d
}

// User returns the user service
func (d *Driver) User() *UserService {
	return d.user
}

// Group returns the group service
func (d *Driver) Group() *GroupService {
	return d.group
}

// ValidateUser validates user credentials
func (d *Driver) ValidateUser(ctx context.Context, username, password string) (*User, error) {
	return d.user.ValidateCredentialsWithConfig(ctx, username, password, d.hashConfig)
}

// GetUserGroups returns groups for a user
func (d *Driver) GetUserGroups(ctx context.Context, userID string) ([]*Group, error) {
	return d.group.GetUserGroups(ctx, userID)
}

// GetUser retrieves a user by ID
func (d *Driver) GetUser(ctx context.Context, id string) (*User, error) {
	return d.user.GetByID(ctx, id)
}

// GetUserByUsername retrieves a user by username
func (d *Driver) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return d.user.GetByUsername(ctx, username)
}

// GetGroupACL returns the combined ACL for groups
func (d *Driver) GetGroupACL(groups []*Group) *GroupACL {
	resolver := acl.NewResolver(d.env)
	return resolver.ResolveForGroups(groups)
}

// CheckAccess checks if user has access to a service/API
func (d *Driver) CheckAccess(ctx context.Context, userID, service, version, api string) (bool, error) {
	groups, err := d.group.GetUserGroups(ctx, userID)
	if err != nil {
		return false, err
	}

	return d.group.CheckAccess(groups, d.env, service, version, api), nil
}

// CheckTenantAccess checks user access to a tenant and returns access info
func (d *Driver) CheckTenantAccess(user *User, tenantID, tenantCode string, roamingTenantID string, autoRoaming map[string]map[string]*AutoRoamingConfig) *TenantAccess {
	return d.user.CheckTenantAccess(user, tenantID, tenantCode, roamingTenantID, autoRoaming)
}

// GetACLForUser gets the combined ACL for a user
func (d *Driver) GetACLForUser(ctx context.Context, user *User, tenantID string) (*GroupACL, error) {
	return d.group.GetACLForUser(ctx, user, tenantID, d.env)
}

// SetEnvironment sets the environment for ACL resolution
func (d *Driver) SetEnvironment(env string) {
	d.env = env
}

// GetEnvironment returns the current environment
func (d *Driver) GetEnvironment() string {
	return d.env
}

// AuthenticateAndGetACL authenticates user and returns their ACL for a tenant
func (d *Driver) AuthenticateAndGetACL(ctx context.Context, username, password, tenantID, tenantCode string) (*User, *TenantAccess, *GroupACL, error) {
	// Validate credentials
	user, err := d.ValidateUser(ctx, username, password)
	if err != nil {
		return nil, nil, nil, err
	}

	// Check tenant access
	access := d.CheckTenantAccess(user, tenantID, tenantCode, "", nil)
	if access == nil {
		return user, nil, nil, ErrNoTenantAccess
	}

	// Get ACL
	acl, err := d.GetACLForUser(ctx, user, tenantID)
	if err != nil {
		return user, access, nil, err
	}

	return user, access, acl, nil
}

// Re-export model types for convenience
type (
	// TenantRef is a reference to a tenant
	TenantRef = model.TenantRef
	// UserConfig holds user configuration
	UserConfig = model.UserConfig
	// AllowedTenant represents a tenant the user can access
	AllowedTenant = model.AllowedTenant
	// GroupConfig holds group configuration
	GroupConfig = model.GroupConfig
	// EnvironmentACL is ACL for an environment
	EnvironmentACL = model.EnvironmentACL
	// ServiceACL is ACL for a service
	ServiceACL = model.ServiceACL
)
