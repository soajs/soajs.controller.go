package model

// User represents a URAC user with full SOAJS schema
type User struct {
	ID           string        `json:"_id" bson:"_id"`
	Username     string        `json:"username" bson:"username"`
	FirstName    string        `json:"firstName" bson:"firstName"`
	LastName     string        `json:"lastName" bson:"lastName"`
	Email        string        `json:"email" bson:"email"`
	Phone        string        `json:"phone,omitempty" bson:"phone,omitempty"`
	Password     string        `json:"-" bson:"password"`
	Status       string        `json:"status" bson:"status"`
	Tenant       TenantRef     `json:"tenant" bson:"tenant"`
	Groups       []string      `json:"groups" bson:"groups"`
	GroupsConfig *GroupsConfig `json:"groupsConfig,omitempty" bson:"groupsConfig,omitempty"`
	Profile      interface{}   `json:"profile,omitempty" bson:"profile,omitempty"`
	Config       *UserConfig   `json:"config,omitempty" bson:"config,omitempty"`
	SocialLogin  *SocialLogin  `json:"socialLogin,omitempty" bson:"socialLogin,omitempty"`
	CreatedAt    interface{}   `json:"ts,omitempty" bson:"ts,omitempty"`
	LastLogin    interface{}   `json:"lastLogin,omitempty" bson:"lastLogin,omitempty"`
	Locked       bool          `json:"locked,omitempty" bson:"locked,omitempty"`
	LoginMode    string        `json:"loginMode,omitempty" bson:"loginMode,omitempty"`
}

// GroupsConfig holds merged configuration from user's groups
type GroupsConfig struct {
	// AllowedPackages maps product code to array of allowed package codes
	AllowedPackages map[string][]string `json:"allowedPackages,omitempty" bson:"allowedPackages,omitempty"`
}

// TenantRef holds tenant reference
type TenantRef struct {
	ID   string `json:"id" bson:"id"`
	Code string `json:"code" bson:"code"`
}

// UserConfig holds user-specific configuration
type UserConfig struct {
	AllowedTenants []AllowedTenant        `json:"allowedTenants,omitempty" bson:"allowedTenants,omitempty"`
	Packages       map[string]interface{} `json:"packages,omitempty" bson:"packages,omitempty"`
	Keys           []UserKey              `json:"keys,omitempty" bson:"keys,omitempty"`
}

// AllowedTenant represents a tenant the user has access to
type AllowedTenant struct {
	Tenant TenantRef `json:"tenant" bson:"tenant"`
	Groups []string  `json:"groups,omitempty" bson:"groups,omitempty"`
}

// UserKey holds user-specific key configuration
type UserKey struct {
	Key    string                 `json:"key" bson:"key"`
	Config map[string]interface{} `json:"config,omitempty" bson:"config,omitempty"`
}

// SocialLogin holds social login information
type SocialLogin struct {
	Strategy string `json:"strategy,omitempty" bson:"strategy,omitempty"`
	ID       string `json:"id,omitempty" bson:"id,omitempty"`
}

// UserStatus constants
const (
	UserStatusActive     = "active"
	UserStatusInactive   = "inactive"
	UserStatusPending    = "pending"
	UserStatusPendingNew = "pendingNew"
)

// IsActive checks if user is active
func (u *User) IsActive() bool {
	return u.Status == UserStatusActive
}

// IsPending checks if user is pending
func (u *User) IsPending() bool {
	return u.Status == UserStatusPending || u.Status == UserStatusPendingNew
}

// IsLocked checks if user is locked
func (u *User) IsLocked() bool {
	return u.Locked
}

// CanLogin checks if user can login
func (u *User) CanLogin() bool {
	return u.IsActive() && !u.IsLocked()
}

// GetFullName returns the user's full name
func (u *User) GetFullName() string {
	if u.FirstName == "" && u.LastName == "" {
		return u.Username
	}
	if u.FirstName == "" {
		return u.LastName
	}
	if u.LastName == "" {
		return u.FirstName
	}
	return u.FirstName + " " + u.LastName
}

// HasTenantAccess checks if user has access to a specific tenant
func (u *User) HasTenantAccess(tenantID string) bool {
	// Check primary tenant
	if u.Tenant.ID == tenantID {
		return true
	}

	// Check allowed tenants
	if u.Config != nil {
		for _, at := range u.Config.AllowedTenants {
			if at.Tenant.ID == tenantID {
				return true
			}
		}
	}

	return false
}

// GetGroupsForTenant returns groups for a specific tenant
func (u *User) GetGroupsForTenant(tenantID string) []string {
	// Primary tenant
	if u.Tenant.ID == tenantID {
		return u.Groups
	}

	// Check allowed tenants
	if u.Config != nil {
		for _, at := range u.Config.AllowedTenants {
			if at.Tenant.ID == tenantID {
				return at.Groups
			}
		}
	}

	return nil
}

// TenantAccess represents user's access to a tenant
type TenantAccess struct {
	Groups []string  `json:"groups"`
	Tenant TenantRef `json:"tenant"`
}

// CheckTenantAccess verifies user access to a tenant and returns access info
func (u *User) CheckTenantAccess(tenantID, tenantCode string, roamingTenantID string) *TenantAccess {
	// Check primary tenant
	if u.Tenant.ID == tenantID {
		return &TenantAccess{
			Groups: u.Groups,
			Tenant: u.Tenant,
		}
	}

	// Check allowed tenants
	if u.Config != nil {
		for _, at := range u.Config.AllowedTenants {
			if at.Tenant.ID == tenantID {
				return &TenantAccess{
					Groups: at.Groups,
					Tenant: at.Tenant,
				}
			}
		}
	}

	// Check roaming
	if roamingTenantID != "" && u.Tenant.ID == roamingTenantID {
		return &TenantAccess{
			Groups: u.Groups,
			Tenant: u.Tenant,
		}
	}

	return nil
}
