package urac

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"golang.org/x/crypto/bcrypt"
)

// User represents a URAC user
type User struct {
	ID        string    `json:"_id" bson:"_id"`
	Username  string    `json:"username" bson:"username"`
	FirstName string    `json:"firstName" bson:"firstName"`
	LastName  string    `json:"lastName" bson:"lastName"`
	Email     string    `json:"email" bson:"email"`
	Password  string    `json:"-" bson:"password"`
	Status    string    `json:"status" bson:"status"`
	Groups    []string  `json:"groups" bson:"groups"`
	LastLogin time.Time `json:"lastLogin" bson:"lastLogin"`
}

// UserService provides user operations
type UserService struct {
	driver *Driver
}

// GetByID retrieves a user by ID
func (s *UserService) GetByID(ctx context.Context, id string) (*User, error) {
	coll := s.driver.mongo.Collection("users")

	var user User
	err := coll.FindOne(ctx, bson.M{"_id": id}).Decode(&user)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return &user, nil
}

// GetByUsername retrieves a user by username
func (s *UserService) GetByUsername(ctx context.Context, username string) (*User, error) {
	coll := s.driver.mongo.Collection("users")

	var user User
	err := coll.FindOne(ctx, bson.M{"username": username}).Decode(&user)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return &user, nil
}

// ValidateCredentials validates username and password
func (s *UserService) ValidateCredentials(ctx context.Context, username, password string) (*User, error) {
	user, err := s.GetByUsername(ctx, username)
	if err != nil {
		return nil, err
	}

	// Check status
	if user.Status != "active" {
		return nil, fmt.Errorf("user is not active")
	}

	// Verify password
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	// Update last login
	s.driver.mongo.Collection("users").UpdateOne(
		ctx,
		bson.M{"_id": user.ID},
		bson.M{"$set": bson.M{"lastLogin": time.Now()}},
	)

	return user, nil
}
