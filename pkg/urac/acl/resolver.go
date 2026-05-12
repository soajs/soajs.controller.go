package acl

import (
	"github.com/soajs/soajs.controller.go/pkg/urac/model"
)

// Resolver resolves ACL permissions from multiple sources
type Resolver struct {
	// Environment code
	env string
}

// NewResolver creates a new ACL resolver
func NewResolver(env string) *Resolver {
	return &Resolver{env: env}
}

// AccessResult holds the result of an access check
type AccessResult struct {
	Allowed bool
	Reason  string
}

// ResolveForGroups merges ACLs from multiple groups
func (r *Resolver) ResolveForGroups(groups []*model.Group) *model.GroupACL {
	if len(groups) == 0 {
		return nil
	}

	merged := &model.GroupACL{
		Environments: make(map[string]*model.EnvironmentACL),
	}

	for _, group := range groups {
		if group == nil {
			continue
		}

		// Merge ACL from each package assignment
		for _, pkg := range group.Packages {
			if pkg.ACL != nil {
				merged.MergeACL(pkg.ACL)
			}
		}
	}

	return merged
}

// CheckAccess checks if access is allowed for the given parameters
func (r *Resolver) CheckAccess(acl *model.GroupACL, service, version, api string) *AccessResult {
	if acl == nil {
		return &AccessResult{Allowed: false, Reason: "no ACL defined"}
	}

	if acl.HasAccess(r.env, service, version, api) {
		return &AccessResult{Allowed: true, Reason: "access granted"}
	}

	return &AccessResult{Allowed: false, Reason: "access denied by ACL"}
}

// MergeACLs merges multiple ACLs with the later ones taking precedence
func MergeACLs(acls ...*model.GroupACL) *model.GroupACL {
	if len(acls) == 0 {
		return nil
	}

	merged := &model.GroupACL{
		Environments: make(map[string]*model.EnvironmentACL),
	}

	for _, acl := range acls {
		merged.MergeACL(acl)
	}

	return merged
}

// PackageACL represents ACL from a product package
type PackageACL struct {
	Product string          `json:"product"`
	Package string          `json:"package"`
	ACL     *model.GroupACL `json:"acl"`
}

// ResolvePackageACL resolves ACL for a specific product/package
func (r *Resolver) ResolvePackageACL(groups []*model.Group, product, pkg string) *model.GroupACL {
	for _, group := range groups {
		if group == nil {
			continue
		}

		for _, groupPkg := range group.Packages {
			if groupPkg.Product == product && groupPkg.Package == pkg {
				if groupPkg.ACL != nil {
					return groupPkg.ACL
				}
			}
		}
	}

	return nil
}

// CheckServiceAccess checks access to a specific service
func (r *Resolver) CheckServiceAccess(acl *model.GroupACL, service string) bool {
	if acl == nil || acl.Environments == nil {
		return false
	}

	envACL, exists := acl.Environments[r.env]
	if !exists || envACL == nil {
		return false
	}

	if !envACL.Access {
		return false
	}

	if envACL.Services == nil {
		return true
	}

	svcACL, exists := envACL.Services[service]
	if !exists {
		return true // Service not explicitly restricted
	}

	if svcACL == nil {
		return true
	}

	if access, ok := svcACL.Access.(bool); ok {
		return access
	}

	return true
}

// CheckAPIAccess checks access to a specific API endpoint
func (r *Resolver) CheckAPIAccess(acl *model.GroupACL, service, version, method, route string) bool {
	if acl == nil {
		return false
	}

	return acl.HasAccess(r.env, service, version, route)
}

// GetAllowedAPIs returns list of allowed API routes for a service
func (r *Resolver) GetAllowedAPIs(acl *model.GroupACL, service, version string) []string {
	if acl == nil || acl.Environments == nil {
		return nil
	}

	envACL, exists := acl.Environments[r.env]
	if !exists || envACL == nil || envACL.Services == nil {
		return nil
	}

	svcACL, exists := envACL.Services[service]
	if !exists || svcACL == nil {
		return nil
	}

	var apis []string

	// Check version-specific APIs
	if svcACL.Versions != nil && version != "" {
		if verACL, exists := svcACL.Versions[version]; exists && verACL != nil && verACL.APIs != nil {
			for api, apiACL := range verACL.APIs {
				if apiACL != nil {
					if access, ok := apiACL.Access.(bool); !ok || access {
						apis = append(apis, api)
					}
				}
			}
			return apis
		}
	}

	// Check service-level APIs
	if svcACL.APIs != nil {
		for api, apiACL := range svcACL.APIs {
			if apiACL != nil {
				if access, ok := apiACL.Access.(bool); !ok || access {
					apis = append(apis, api)
				}
			}
		}
	}

	return apis
}

// ACLCheckParams holds parameters for ACL check
type ACLCheckParams struct {
	Environment string
	Service     string
	Version     string
	Method      string
	Route       string
	Groups      []*model.Group
}

// FullAccessCheck performs a complete access check
func FullAccessCheck(params ACLCheckParams) *AccessResult {
	resolver := NewResolver(params.Environment)
	acl := resolver.ResolveForGroups(params.Groups)
	return resolver.CheckAccess(acl, params.Service, params.Version, params.Route)
}
