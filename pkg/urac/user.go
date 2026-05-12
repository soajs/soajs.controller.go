package urac

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/urac/hasher"
	"github.com/soajs/soajs.controller.go/pkg/urac/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Common errors
var (
	ErrUserNotFound     = errors.New("user not found")
	ErrUserNotActive    = errors.New("user is not active")
	ErrUserLocked       = errors.New("user is locked")
	ErrInvalidPassword  = errors.New("invalid password")
	ErrNoTenantAccess   = errors.New("user has no access to tenant")
)

// User represents a URAC user (re-exported from model)
type User = model.User

// TenantAccess represents user's access to a tenant
type TenantAccess = model.TenantAccess

// UserService provides user operations
type UserService struct {
	driver *Driver
}

// HasherConfig holds hasher configuration from registry
type HasherConfig struct {
	HashIterations    int    `json:"hashIterations"`
	OptionalAlgorithm string `json:"optionalAlgorithm"`
}

// GetByID retrieves a user by ID
func (s *UserService) GetByID(ctx context.Context, id string) (*User, error) {
	coll := s.driver.mongo.Collection("users")

	var user User
	err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetByUsername retrieves a user by username
func (s *UserService) GetByUsername(ctx context.Context, username string) (*User, error) {
	coll := s.driver.mongo.Collection("users")

	var user User
	err := coll.FindOne(ctx, bson.M{"username": username}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetByEmail retrieves a user by email
func (s *UserService) GetByEmail(ctx context.Context, email string) (*User, error) {
	coll := s.driver.mongo.Collection("users")

	var user User
	err := coll.FindOne(ctx, bson.M{"email": email}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// GetByUsernameOrEmail retrieves a user by username or email
func (s *UserService) GetByUsernameOrEmail(ctx context.Context, identifier string) (*User, error) {
	coll := s.driver.mongo.Collection("users")

	var user User
	err := coll.FindOne(ctx, bson.M{
		"$or": []bson.M{
			{"username": identifier},
			{"email": identifier},
		},
	}).Decode(&user)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}

	return &user, nil
}

// ValidateCredentials validates username and password
func (s *UserService) ValidateCredentials(ctx context.Context, username, password string) (*User, error) {
	return s.ValidateCredentialsWithConfig(ctx, username, password, hasher.DefaultConfig())
}

// ValidateCredentialsWithConfig validates username and password with custom hasher config
func (s *UserService) ValidateCredentialsWithConfig(ctx context.Context, username, password string, hashCfg hasher.Config) (*User, error) {
	user, err := s.GetByUsernameOrEmail(ctx, username)
	if err != nil {
		return nil, err
	}

	// Check status
	if !user.IsActive() {
		return nil, ErrUserNotActive
	}

	// Check locked
	if user.IsLocked() {
		return nil, ErrUserLocked
	}

	// Verify password
	h := hasher.New(hashCfg)
	if err := h.Compare(password, user.Password); err != nil {
		if errors.Is(err, hasher.ErrPasswordMismatch) {
			return nil, ErrInvalidPassword
		}
		return nil, fmt.Errorf("password verification failed: %w", err)
	}

	// Update last login
	go s.updateLastLogin(ctx, user.ID)

	return user, nil
}

// updateLastLogin updates the user's last login time
func (s *UserService) updateLastLogin(ctx context.Context, userID string) {
	coll := s.driver.mongo.Collection("users")
	coll.UpdateOne(
		ctx,
		bson.M{"_id": userID},
		bson.M{"$set": bson.M{"lastLogin": time.Now()}},
	)
}

// CheckTenantAccess checks if user has access to a tenant
func (s *UserService) CheckTenantAccess(user *User, tenantID, tenantCode string, roamingTenantID string, autoRoaming map[string]map[string]*AutoRoamingConfig) *TenantAccess {
	if user == nil {
		return nil
	}

	// Check primary tenant
	if user.Tenant.ID == tenantID {
		return &TenantAccess{
			Groups: user.Groups,
			Tenant: user.Tenant,
		}
	}

	// Check allowed tenants
	if user.Config != nil {
		for _, at := range user.Config.AllowedTenants {
			if at.Tenant.ID == tenantID {
				return &TenantAccess{
					Groups: at.Groups,
					Tenant: at.Tenant,
				}
			}
		}
	}

	// Check roaming
	if roamingTenantID != "" && user.Tenant.ID == roamingTenantID {
		return &TenantAccess{
			Groups: user.Groups,
			Tenant: user.Tenant,
		}
	}

	// Check auto-roaming
	if autoRoaming != nil {
		if tenantRoaming, exists := autoRoaming[tenantID]; exists {
			if userTenantRoaming, exists := tenantRoaming[user.Tenant.ID]; exists {
				return &TenantAccess{
					Groups: userTenantRoaming.Groups,
					Tenant: model.TenantRef{
						ID:   tenantID,
						Code: tenantCode,
					},
				}
			}
		}
	}

	return nil
}

// AutoRoamingConfig holds auto-roaming configuration
type AutoRoamingConfig struct {
	Groups []string `json:"groups"`
}

// GetUsersByTenant retrieves all users for a tenant
func (s *UserService) GetUsersByTenant(ctx context.Context, tenantID string) ([]*User, error) {
	coll := s.driver.mongo.Collection("users")

	cursor, err := coll.Find(ctx, bson.M{"tenant.id": tenantID})
	if err != nil {
		return nil, fmt.Errorf("failed to find users: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, fmt.Errorf("failed to decode users: %w", err)
	}

	return users, nil
}

// GetUsersByGroup retrieves all users in a specific group
func (s *UserService) GetUsersByGroup(ctx context.Context, groupCode string) ([]*User, error) {
	coll := s.driver.mongo.Collection("users")

	cursor, err := coll.Find(ctx, bson.M{"groups": groupCode})
	if err != nil {
		return nil, fmt.Errorf("failed to find users: %w", err)
	}
	defer cursor.Close(ctx)

	var users []*User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, fmt.Errorf("failed to decode users: %w", err)
	}

	return users, nil
}

// CountUsers counts users matching the filter
func (s *UserService) CountUsers(ctx context.Context, filter bson.M) (int64, error) {
	coll := s.driver.mongo.Collection("users")
	return coll.CountDocuments(ctx, filter)
}

// CountActiveUsers counts active users for a tenant
func (s *UserService) CountActiveUsers(ctx context.Context, tenantID string) (int64, error) {
	return s.CountUsers(ctx, bson.M{
		"tenant.id": tenantID,
		"status":    model.UserStatusActive,
	})
}
