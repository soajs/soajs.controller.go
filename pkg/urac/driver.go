package urac

import (
	"context"

	"github.com/soajs/soajs.controller.go/pkg/mongo"
	"go.uber.org/zap"
)

// Driver provides URAC functionality
type Driver struct {
	mongo  *mongo.Client
	logger *zap.Logger
	user   *UserService
	group  *GroupService
}

// NewDriver creates a new URAC driver
func NewDriver(mongoClient *mongo.Client, logger *zap.Logger) *Driver {
	d := &Driver{
		mongo:  mongoClient,
		logger: logger,
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
	return d.user.ValidateCredentials(ctx, username, password)
}

// GetUserGroups returns groups for a user
func (d *Driver) GetUserGroups(ctx context.Context, userID string) ([]*Group, error) {
	return d.group.GetUserGroups(ctx, userID)
}
