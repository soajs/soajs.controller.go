package model

import (
	"testing"
)

func TestUser_IsActive(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"active user", UserStatusActive, true},
		{"inactive user", UserStatusInactive, false},
		{"pending user", UserStatusPending, false},
		{"pending new user", UserStatusPendingNew, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Status: tt.status}
			if got := u.IsActive(); got != tt.want {
				t.Errorf("User.IsActive() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_IsPending(t *testing.T) {
	tests := []struct {
		name   string
		status string
		want   bool
	}{
		{"active user", UserStatusActive, false},
		{"inactive user", UserStatusInactive, false},
		{"pending user", UserStatusPending, true},
		{"pending new user", UserStatusPendingNew, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Status: tt.status}
			if got := u.IsPending(); got != tt.want {
				t.Errorf("User.IsPending() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_IsLocked(t *testing.T) {
	tests := []struct {
		name   string
		locked bool
		want   bool
	}{
		{"locked user", true, true},
		{"unlocked user", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Locked: tt.locked}
			if got := u.IsLocked(); got != tt.want {
				t.Errorf("User.IsLocked() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_CanLogin(t *testing.T) {
	tests := []struct {
		name   string
		status string
		locked bool
		want   bool
	}{
		{"active unlocked", UserStatusActive, false, true},
		{"active locked", UserStatusActive, true, false},
		{"inactive unlocked", UserStatusInactive, false, false},
		{"pending unlocked", UserStatusPending, false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{Status: tt.status, Locked: tt.locked}
			if got := u.CanLogin(); got != tt.want {
				t.Errorf("User.CanLogin() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_GetFullName(t *testing.T) {
	tests := []struct {
		name      string
		firstName string
		lastName  string
		username  string
		want      string
	}{
		{"both names", "John", "Doe", "johnd", "John Doe"},
		{"first only", "John", "", "johnd", "John"},
		{"last only", "", "Doe", "johnd", "Doe"},
		{"no names", "", "", "johnd", "johnd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u := &User{
				FirstName: tt.firstName,
				LastName:  tt.lastName,
				Username:  tt.username,
			}
			if got := u.GetFullName(); got != tt.want {
				t.Errorf("User.GetFullName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_HasTenantAccess(t *testing.T) {
	user := &User{
		Tenant: TenantRef{ID: "tenant1", Code: "T1"},
		Config: &UserConfig{
			AllowedTenants: []AllowedTenant{
				{Tenant: TenantRef{ID: "tenant2", Code: "T2"}, Groups: []string{"group1"}},
			},
		},
	}

	tests := []struct {
		name     string
		tenantID string
		want     bool
	}{
		{"primary tenant", "tenant1", true},
		{"allowed tenant", "tenant2", true},
		{"unknown tenant", "tenant3", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := user.HasTenantAccess(tt.tenantID); got != tt.want {
				t.Errorf("User.HasTenantAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_GetGroupsForTenant(t *testing.T) {
	user := &User{
		Tenant: TenantRef{ID: "tenant1", Code: "T1"},
		Groups: []string{"admin", "user"},
		Config: &UserConfig{
			AllowedTenants: []AllowedTenant{
				{Tenant: TenantRef{ID: "tenant2", Code: "T2"}, Groups: []string{"guest"}},
			},
		},
	}

	tests := []struct {
		name     string
		tenantID string
		want     []string
	}{
		{"primary tenant", "tenant1", []string{"admin", "user"}},
		{"allowed tenant", "tenant2", []string{"guest"}},
		{"unknown tenant", "tenant3", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := user.GetGroupsForTenant(tt.tenantID)
			if len(got) != len(tt.want) {
				t.Errorf("User.GetGroupsForTenant() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestUser_CheckTenantAccess(t *testing.T) {
	user := &User{
		Tenant: TenantRef{ID: "tenant1", Code: "T1"},
		Groups: []string{"admin"},
		Config: &UserConfig{
			AllowedTenants: []AllowedTenant{
				{Tenant: TenantRef{ID: "tenant2", Code: "T2"}, Groups: []string{"guest"}},
			},
		},
	}

	tests := []struct {
		name            string
		tenantID        string
		tenantCode      string
		roamingTenantID string
		wantAccess      bool
	}{
		{"primary tenant", "tenant1", "T1", "", true},
		{"allowed tenant", "tenant2", "T2", "", true},
		{"unknown tenant", "tenant3", "T3", "", false},
		{"roaming tenant", "tenant3", "T3", "tenant1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			access := user.CheckTenantAccess(tt.tenantID, tt.tenantCode, tt.roamingTenantID)
			hasAccess := access != nil
			if hasAccess != tt.wantAccess {
				t.Errorf("User.CheckTenantAccess() access = %v, want %v", hasAccess, tt.wantAccess)
			}
		})
	}
}

func TestUser_NoConfig(t *testing.T) {
	user := &User{
		Tenant: TenantRef{ID: "tenant1", Code: "T1"},
		Groups: []string{"admin"},
	}

	// Should not panic with nil config
	if !user.HasTenantAccess("tenant1") {
		t.Error("Should have access to primary tenant")
	}

	if user.HasTenantAccess("tenant2") {
		t.Error("Should not have access to other tenants")
	}

	groups := user.GetGroupsForTenant("tenant2")
	if groups != nil {
		t.Error("Should return nil for unknown tenant")
	}
}
