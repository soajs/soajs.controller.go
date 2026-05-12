package urac

import (
	"context"
	"fmt"

	"go.mongodb.org/mongo-driver/bson"
)

// Group represents a URAC group
type Group struct {
	ID          string `json:"_id" bson:"_id"`
	Code        string `json:"code" bson:"code"`
	Name        string `json:"name" bson:"name"`
	Description string `json:"description" bson:"description"`
}

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
		return nil, fmt.Errorf("group not found: %w", err)
	}

	return &group, nil
}

// GetByCode retrieves a group by code
func (s *GroupService) GetByCode(ctx context.Context, code string) (*Group, error) {
	coll := s.driver.mongo.Collection("groups")

	var group Group
	err := coll.FindOne(ctx, bson.M{"code": code}).Decode(&group)
	if err != nil {
		return nil, fmt.Errorf("group not found: %w", err)
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

	if len(user.Groups) == 0 {
		return []*Group{}, nil
	}

	// Get all groups
	coll := s.driver.mongo.Collection("groups")
	cursor, err := coll.Find(ctx, bson.M{"code": bson.M{"$in": user.Groups}})
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
