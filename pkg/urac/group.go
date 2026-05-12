package urac

import (
	"context"
	"errors"
	"fmt"

	"github.com/soajs/soajs.controller.go/pkg/urac/acl"
	"github.com/soajs/soajs.controller.go/pkg/urac/model"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// Common group errors
var (
	ErrGroupNotFound = errors.New("group not found")
)

// Group represents a URAC group (re-exported from model)
type Group = model.Group

// GroupACL represents group ACL (re-exported from model)
type GroupACL = model.GroupACL

// GroupService provides group operations
type GroupService struct {
	driver *Driver
}

// GetByID retrieves a group by ID
func (s *GroupService) GetByID(ctx context.Context, id string) (*Group, error) {
	coll := s.driver.mongo.Collection("groups")

	var group Group
	err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&group)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	return &group, nil
}

// GetByCode retrieves a group by code
func (s *GroupService) GetByCode(ctx context.Context, code string) (*Group, error) {
	coll := s.driver.mongo.Collection("groups")

	var group Group
	err := coll.FindOne(ctx, bson.M{"code": code}).Decode(&group)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	return &group, nil
}

// GetByCodeAndTenant retrieves a group by code and tenant
func (s *GroupService) GetByCodeAndTenant(ctx context.Context, code, tenantID string) (*Group, error) {
	coll := s.driver.mongo.Collection("groups")

	var group Group
	err := coll.FindOne(ctx, bson.M{
		"code":      code,
		"tenant.id": tenantID,
	}).Decode(&group)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrGroupNotFound
		}
		return nil, fmt.Errorf("failed to get group: %w", err)
	}

	return &group, nil
}

// GetUserGroups retrieves all groups for a user
func (s *GroupService) GetUserGroups(ctx context.Context, userID string) ([]*Group, error) {
	// First get the user to get group codes
	user, err := s.driver.user.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}

	return s.GetGroupsByCodes(ctx, user.Groups)
}

// GetGroupsByCodes retrieves groups by their codes
func (s *GroupService) GetGroupsByCodes(ctx context.Context, codes []string) ([]*Group, error) {
	if len(codes) == 0 {
		return []*Group{}, nil
	}

	coll := s.driver.mongo.Collection("groups")
	cursor, err := coll.Find(ctx, bson.M{"code": bson.M{"$in": codes}})
	if err != nil {
		return nil, fmt.Errorf("failed to find groups: %w", err)
	}
	defer cursor.Close(ctx)

	var groups []*Group
	if err := cursor.All(ctx, &groups); err != nil {
		return nil, fmt.Errorf("failed to decode groups: %w", err)
	}

	return groups, nil
}

// GetGroupsByTenant retrieves all groups for a tenant
func (s *GroupService) GetGroupsByTenant(ctx context.Context, tenantID string) ([]*Group, error) {
	coll := s.driver.mongo.Collection("groups")

	cursor, err := coll.Find(ctx, bson.M{"tenant.id": tenantID})
	if err != nil {
		return nil, fmt.Errorf("failed to find groups: %w", err)
	}
	defer cursor.Close(ctx)

	var groups []*Group
	if err := cursor.All(ctx, &groups); err != nil {
		return nil, fmt.Errorf("failed to decode groups: %w", err)
	}

	return groups, nil
}

// GetGroupsForUserAndTenant retrieves groups for a user within a specific tenant
func (s *GroupService) GetGroupsForUserAndTenant(ctx context.Context, user *User, tenantID string) ([]*Group, error) {
	// Get group codes for this tenant
	groupCodes := user.GetGroupsForTenant(tenantID)
	if len(groupCodes) == 0 {
		return []*Group{}, nil
	}

	// Get group details
	return s.GetGroupsByCodes(ctx, groupCodes)
}

// ResolveACL resolves the combined ACL for a set of groups
func (s *GroupService) ResolveACL(groups []*Group, env string) *GroupACL {
	resolver := acl.NewResolver(env)
	return resolver.ResolveForGroups(groups)
}

// CheckAccess checks if the given groups allow access to a service/API
func (s *GroupService) CheckAccess(groups []*Group, env, service, version, api string) bool {
	resolver := acl.NewResolver(env)
	resolvedACL := resolver.ResolveForGroups(groups)
	result := resolver.CheckAccess(resolvedACL, service, version, api)
	return result.Allowed
}

// GetACLForUser gets the combined ACL for a user in a specific tenant/environment
func (s *GroupService) GetACLForUser(ctx context.Context, user *User, tenantID, env string) (*GroupACL, error) {
	groups, err := s.GetGroupsForUserAndTenant(ctx, user, tenantID)
	if err != nil {
		return nil, err
	}

	return s.ResolveACL(groups, env), nil
}

// HasAccessToService checks if user has access to a service
func (s *GroupService) HasAccessToService(ctx context.Context, user *User, tenantID, env, service string) (bool, error) {
	groups, err := s.GetGroupsForUserAndTenant(ctx, user, tenantID)
	if err != nil {
		return false, err
	}

	resolver := acl.NewResolver(env)
	resolvedACL := resolver.ResolveForGroups(groups)
	return resolver.CheckServiceAccess(resolvedACL, service), nil
}

// GetAllowedServices returns list of services the user can access
func (s *GroupService) GetAllowedServices(ctx context.Context, user *User, tenantID, env string) ([]string, error) {
	groups, err := s.GetGroupsForUserAndTenant(ctx, user, tenantID)
	if err != nil {
		return nil, err
	}

	resolver := acl.NewResolver(env)
	resolvedACL := resolver.ResolveForGroups(groups)

	if resolvedACL == nil {
		return nil, nil
	}

	return resolvedACL.GetAllowedServices(env), nil
}
