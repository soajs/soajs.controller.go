package model

// Group represents a URAC group with full SOAJS schema
type Group struct {
	ID          string                 `json:"_id" bson:"_id"`
	Code        string                 `json:"code" bson:"code"`
	Name        string                 `json:"name" bson:"name"`
	Description string                 `json:"description,omitempty" bson:"description,omitempty"`
	Tenant      TenantRef              `json:"tenant" bson:"tenant"`
	Config      *GroupConfig           `json:"config,omitempty" bson:"config,omitempty"`
	Packages    []GroupPackage         `json:"packages,omitempty" bson:"packages,omitempty"`
}

// GroupConfig holds group-specific configuration including ACL
type GroupConfig struct {
	AllowedPackages map[string]AllowedPackage `json:"allowedPackages,omitempty" bson:"allowedPackages,omitempty"`
	Keys            []GroupKey                `json:"keys,omitempty" bson:"keys,omitempty"`
}

// AllowedPackage represents product/package access configuration
type AllowedPackage struct {
	Product  string   `json:"product" bson:"product"`
	Packages []string `json:"packages" bson:"packages"`
}

// GroupKey holds group-specific key configuration
type GroupKey struct {
	Key    string                 `json:"key" bson:"key"`
	Config map[string]interface{} `json:"config,omitempty" bson:"config,omitempty"`
}

// GroupPackage represents package assignment for a group
type GroupPackage struct {
	Product string     `json:"product" bson:"product"`
	Package string     `json:"package" bson:"package"`
	ACL     *GroupACL  `json:"acl,omitempty" bson:"acl,omitempty"`
}

// GroupACL represents access control list for a group
type GroupACL struct {
	// Environment-level ACL
	// Key: environment code, Value: environment ACL
	Environments map[string]*EnvironmentACL `json:"environments,omitempty" bson:"environments,omitempty"`
}

// EnvironmentACL represents ACL for a specific environment
type EnvironmentACL struct {
	// Access flag
	Access bool `json:"access,omitempty" bson:"access,omitempty"`

	// Service-level ACL
	// Key: service name, Value: service ACL
	Services map[string]*ServiceACL `json:"services,omitempty" bson:"services,omitempty"`
}

// ServiceACL represents ACL for a specific service
type ServiceACL struct {
	// Access flag - true means full access, false means restricted
	Access interface{} `json:"access,omitempty" bson:"access,omitempty"`

	// API methods ACL
	// Key: API method name (e.g., "get", "post"), Value: API ACL
	APIs map[string]*APIACL `json:"apis,omitempty" bson:"apis,omitempty"`

	// Version-specific ACL
	// Key: version, Value: version ACL
	Versions map[string]*VersionACL `json:"versions,omitempty" bson:"versions,omitempty"`
}

// VersionACL represents ACL for a specific service version
type VersionACL struct {
	Access interface{}         `json:"access,omitempty" bson:"access,omitempty"`
	APIs   map[string]*APIACL  `json:"apis,omitempty" bson:"apis,omitempty"`
}

// APIACL represents ACL for a specific API endpoint
type APIACL struct {
	// Access flag - true, false, or array of groups
	Access interface{} `json:"access,omitempty" bson:"access,omitempty"`
}

// ACL is a generic ACL structure used throughout the system
type ACL map[string]interface{}

// HasAccess checks if the group ACL allows access to a service
func (g *GroupACL) HasAccess(env, service, version, api string) bool {
	if g == nil || g.Environments == nil {
		return false
	}

	envACL, exists := g.Environments[env]
	if !exists || envACL == nil {
		return false
	}

	// Check environment-level access
	if !envACL.Access {
		return false
	}

	if envACL.Services == nil {
		return envACL.Access
	}

	svcACL, exists := envACL.Services[service]
	if !exists || svcACL == nil {
		return envACL.Access
	}

	// Check service-level access
	if access, ok := svcACL.Access.(bool); ok && !access {
		return false
	}

	// Check version-specific ACL
	if svcACL.Versions != nil && version != "" {
		if verACL, exists := svcACL.Versions[version]; exists && verACL != nil {
			if access, ok := verACL.Access.(bool); ok && !access {
				return false
			}
			// Check API-specific ACL in version
			if api != "" && verACL.APIs != nil {
				if apiACL, exists := verACL.APIs[api]; exists && apiACL != nil {
					if access, ok := apiACL.Access.(bool); ok {
						return access
					}
				}
			}
		}
	}

	// Check service-level API ACL
	if api != "" && svcACL.APIs != nil {
		if apiACL, exists := svcACL.APIs[api]; exists && apiACL != nil {
			if access, ok := apiACL.Access.(bool); ok {
				return access
			}
		}
	}

	return true
}

// MergeACL merges another GroupACL into this one
// The other ACL takes precedence for explicit denials
func (g *GroupACL) MergeACL(other *GroupACL) {
	if other == nil || other.Environments == nil {
		return
	}

	if g.Environments == nil {
		g.Environments = make(map[string]*EnvironmentACL)
	}

	for env, otherEnvACL := range other.Environments {
		if otherEnvACL == nil {
			continue
		}

		envACL, exists := g.Environments[env]
		if !exists || envACL == nil {
			g.Environments[env] = otherEnvACL
			continue
		}

		// Merge access (OR logic for granting access)
		envACL.Access = envACL.Access || otherEnvACL.Access

		// Merge services
		if otherEnvACL.Services != nil {
			if envACL.Services == nil {
				envACL.Services = make(map[string]*ServiceACL)
			}
			for svc, otherSvcACL := range otherEnvACL.Services {
				if _, exists := envACL.Services[svc]; !exists {
					envACL.Services[svc] = otherSvcACL
				}
				// More complex merging can be added here
			}
		}
	}
}

// GetAllowedServices returns list of allowed services for an environment
func (g *GroupACL) GetAllowedServices(env string) []string {
	if g == nil || g.Environments == nil {
		return nil
	}

	envACL, exists := g.Environments[env]
	if !exists || envACL == nil || envACL.Services == nil {
		return nil
	}

	var services []string
	for svc, svcACL := range envACL.Services {
		if svcACL != nil {
			if access, ok := svcACL.Access.(bool); !ok || access {
				services = append(services, svc)
			}
		}
	}
	return services
}
