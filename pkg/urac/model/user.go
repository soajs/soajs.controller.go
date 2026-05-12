package model

import "time"

// User represents a URAC user
type User struct {
	ID        string    `json:"_id" bson:"_id"`
	Username  string    `json:"username" bson:"username"`
	FirstName string    `json:"firstName" bson:"firstName"`
	LastName  string    `json:"lastName" bson:"lastName"`
	Email     string    `json:"email" bson:"email"`
	Password  string    `json:"-" bson:"password"`
	Status    string    `json:"status" bson:"status"`
	Tenant    TenantRef `json:"tenant" bson:"tenant"`
	Groups    []string  `json:"groups" bson:"groups"`
	Profile   interface{} `json:"profile" bson:"profile"`
	Config    interface{} `json:"config" bson:"config"`
	CreatedAt time.Time `json:"ts" bson:"ts"`
	LastLogin time.Time `json:"lastLogin" bson:"lastLogin"`
}

// TenantRef holds tenant reference
type TenantRef struct {
	ID   string `json:"id" bson:"id"`
	Code string `json:"code" bson:"code"`
}

// UserStatus constants
const (
	UserStatusActive      = "active"
	UserStatusInactive    = "inactive"
	UserStatusPending     = "pending"
	UserStatusPendingNew  = "pendingNew"
)

// IsActive checks if user is active
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}
