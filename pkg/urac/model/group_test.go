package model

import (
	"testing"
)

func TestGroupACL_HasAccess(t *testing.T) {
	acl := &GroupACL{
		Environments: map[string]*EnvironmentACL{
			"dev": {
				Access: true,
				Services: map[string]*ServiceACL{
					"urac": {
						Access: true,
						APIs: map[string]*APIACL{
							"/user/get": {Access: true},
							"/user/delete": {Access: false},
						},
					},
					"blocked": {
						Access: false,
					},
				},
			},
			"prod": {
				Access: false,
			},
		},
	}

	tests := []struct {
		name    string
		env     string
		service string
		version string
		api     string
		want    bool
	}{
		{"env with access", "dev", "urac", "", "", true},
		{"env without access", "prod", "urac", "", "", false},
		{"unknown env", "staging", "urac", "", "", false},
		{"allowed service", "dev", "urac", "", "", true},
		{"blocked service", "dev", "blocked", "", "", false},
		{"allowed api", "dev", "urac", "", "/user/get", true},
		{"blocked api", "dev", "urac", "", "/user/delete", false},
		{"unknown service (inherits env)", "dev", "unknown", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acl.HasAccess(tt.env, tt.service, tt.version, tt.api); got != tt.want {
				t.Errorf("GroupACL.HasAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupACL_HasAccess_Nil(t *testing.T) {
	var acl *GroupACL
	if acl.HasAccess("dev", "urac", "", "") {
		t.Error("Nil ACL should return false")
	}

	acl = &GroupACL{}
	if acl.HasAccess("dev", "urac", "", "") {
		t.Error("Empty ACL should return false")
	}
}

func TestGroupACL_HasAccess_VersionSpecific(t *testing.T) {
	acl := &GroupACL{
		Environments: map[string]*EnvironmentACL{
			"dev": {
				Access: true,
				Services: map[string]*ServiceACL{
					"api": {
						Access: true,
						Versions: map[string]*VersionACL{
							"1": {
								Access: true,
								APIs: map[string]*APIACL{
									"/v1/endpoint": {Access: true},
								},
							},
							"2": {
								Access: false,
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name    string
		service string
		version string
		api     string
		want    bool
	}{
		{"version 1 allowed", "api", "1", "", true},
		{"version 2 blocked", "api", "2", "", false},
		{"version 1 api allowed", "api", "1", "/v1/endpoint", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := acl.HasAccess("dev", tt.service, tt.version, tt.api); got != tt.want {
				t.Errorf("GroupACL.HasAccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupACL_MergeACL(t *testing.T) {
	acl1 := &GroupACL{
		Environments: map[string]*EnvironmentACL{
			"dev": {
				Access: true,
				Services: map[string]*ServiceACL{
					"service1": {Access: true},
				},
			},
		},
	}

	acl2 := &GroupACL{
		Environments: map[string]*EnvironmentACL{
			"dev": {
				Access: false, // This shouldn't override since OR logic
				Services: map[string]*ServiceACL{
					"service2": {Access: true},
				},
			},
			"prod": {
				Access: true,
			},
		},
	}

	acl1.MergeACL(acl2)

	// Check dev environment still has access (OR logic)
	if !acl1.Environments["dev"].Access {
		t.Error("Merged ACL dev environment should still have access")
	}

	// Check prod was added
	if acl1.Environments["prod"] == nil || !acl1.Environments["prod"].Access {
		t.Error("Merged ACL should have prod environment")
	}

	// Check service2 was added
	if acl1.Environments["dev"].Services["service2"] == nil {
		t.Error("Merged ACL should have service2 in dev")
	}
}

func TestGroupACL_MergeACL_Nil(t *testing.T) {
	acl := &GroupACL{
		Environments: map[string]*EnvironmentACL{
			"dev": {Access: true},
		},
	}

	// Should not panic
	acl.MergeACL(nil)
	acl.MergeACL(&GroupACL{})

	if !acl.Environments["dev"].Access {
		t.Error("ACL should be unchanged after merging nil")
	}
}

func TestGroupACL_GetAllowedServices(t *testing.T) {
	acl := &GroupACL{
		Environments: map[string]*EnvironmentACL{
			"dev": {
				Access: true,
				Services: map[string]*ServiceACL{
					"service1": {Access: true},
					"service2": {Access: true},
					"blocked":  {Access: false},
				},
			},
		},
	}

	services := acl.GetAllowedServices("dev")
	if len(services) != 2 {
		t.Errorf("Expected 2 allowed services, got %d", len(services))
	}

	// Check prod returns nil
	services = acl.GetAllowedServices("prod")
	if services != nil {
		t.Error("Expected nil for unknown environment")
	}
}

func TestGroup_WithPackages(t *testing.T) {
	group := &Group{
		ID:   "group1",
		Code: "admin",
		Name: "Administrators",
		Packages: []GroupPackage{
			{
				Product: "DSBRD",
				Package: "DSBRD_ADMIN",
				ACL: &GroupACL{
					Environments: map[string]*EnvironmentACL{
						"dev": {Access: true},
					},
				},
			},
		},
	}

	if len(group.Packages) != 1 {
		t.Error("Expected 1 package")
	}

	if group.Packages[0].Product != "DSBRD" {
		t.Error("Expected DSBRD product")
	}

	if group.Packages[0].ACL == nil {
		t.Error("Expected ACL to be set")
	}
}
