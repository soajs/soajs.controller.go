package urac

import (
	"context"
	"errors"

	"github.com/soajs/soajs.controller.go/internal/provision"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// Request is what the user of a request is resolved from.
type Request struct {
	Registry *types.RegistryData
	Tenant   *types.Tenant
	Roaming  *Roaming
	Product  string
	// KeyConfig is the key's configuration for the environment.
	KeyConfig map[string]interface{}
	IKey      string
	Token     *types.AccessToken
	// Packages returns a package, as provision getPackageData.
	Packages func(code string) (*types.Package, error)
	// ResolveRequest, when the tenant names an access resolver.
	Resolve *ResolveRequest
	Logger  *zap.Logger
}

// User is the logged-in user of a request (mw/mt/urac.js).
type User struct {
	driver    *Driver
	req       *Request
	record    map[string]interface{}
	acl       *ACL
	id        string
	username  string
	tokenUser map[string]interface{}
	resolver  *Resolver
}

// NewUser prepares the user of a request from its token. The token's user is the record when the
// token was issued with loginMode oauth, when the registry getUserFromToken is on and the token
// was issued to the request's tenant, and when roaming. Otherwise the record is read, or decided
// by the tenant's access resolver when the user switches to another tenant without an
// invitation in the token.
func NewUser(driver *Driver, req *Request) *User {
	u := &User{driver: driver, req: req}
	if req.Token != nil && req.Token.User != nil {
		user := req.Token.User
		u.id, _ = user["id"].(string)
		u.tokenUser = user
		u.username, _ = user["username"].(string)
		getUserFromToken := req.Registry != nil && req.Registry.ServiceConfig != nil && req.Registry.ServiceConfig.OAuth.GetUserFromToken
		switch {
		case user["loginMode"] == "oauth":
			u.record = user
		case getUserFromToken:
			if req.Roaming == nil && req.Tenant != nil && req.Token.ClientID == req.Tenant.ID {
				u.record = user
			}
			if req.Roaming != nil {
				u.record = user
			}
		case req.Roaming != nil:
			u.record = user
		}
	}
	if u.record == nil && u.id != "" && u.tokenUser != nil && req.Tenant != nil && req.Roaming == nil {
		home, _ := u.tokenUser["tenant"].(map[string]interface{})
		if home != nil && home["id"] != req.Tenant.ID && !isInvited(u.tokenUser, req.Tenant.ID) {
			u.resolver = GetResolver(req.Registry, req.Tenant, req.Logger)
		}
	}
	return u
}

// isInvited reports whether the token's user has an invitation to the tenant.
func isInvited(user map[string]interface{}, tenantID string) bool {
	config, _ := user["config"].(map[string]interface{})
	allowed, ok := config["allowedTenants"].([]interface{})
	if !ok {
		return false
	}
	for _, a := range allowed {
		item, _ := a.(map[string]interface{})
		t, _ := item["tenant"].(map[string]interface{})
		if t != nil && t["id"] == tenantID {
			return true
		}
	}
	return false
}

// ErrNoUserID is answered when the token has no user id to read the record by.
var ErrNoUserID = errors.New("oAuth userId is not available to pull URAC profile")

// Init loads the record when needed, then the user's ACL. The error is the urac driver's.
func (u *User) Init(ctx context.Context) (map[string]interface{}, error) {
	if u.record != nil {
		if u.acl == nil {
			u.resolveACL()
		}
		return u.record, nil
	}
	if u.id == "" {
		return nil, ErrNoUserID
	}
	if u.resolver != nil && u.req.Resolve != nil {
		if access := u.resolver.Resolve(ctx, u.req.Resolve, u.tokenUser); access != nil {
			record := make(map[string]interface{}, len(u.tokenUser)+3)
			for k, v := range u.tokenUser {
				record[k] = v
			}
			record["tenant"] = map[string]interface{}{"id": u.req.Tenant.ID, "code": u.req.Tenant.Code}
			record["groups"] = access.Groups
			record["groupsConfig"] = access.GroupsConfig
			u.record = record
			u.resolveACL()
			return u.record, nil
		}
	}
	tenant := Tenant{Roaming: u.req.Roaming}
	if u.req.Tenant != nil {
		tenant.ID, tenant.Code = u.req.Tenant.ID, u.req.Tenant.Code
	}
	record, err := u.driver.GetRecord(ctx, u.req.Registry, tenant, u.username, u.id)
	if err != nil {
		return nil, err
	}
	u.record = record
	u.resolveACL()
	return u.record, nil
}

// resolveACL merges the ACL of the packages the user's groups allow under the request's product.
// A network package override for users (key config gateway.networkPackages.<network>.<product>
// .users) replaces a package by another. No package, or one that does not load, leaves the user
// without an ACL; with several, a package that does not exist is skipped.
func (u *User) resolveACL() {
	allowed := u.AllowedPackages()
	if allowed == nil {
		return
	}
	codes := make([]string, 0, len(allowed))
	for _, c := range allowed {
		s, _ := c.(string)
		codes = append(codes, s)
	}
	if network := lastSeenNetwork(u.req.Registry); network != "" {
		gateway, _ := u.req.KeyConfig["gateway"].(map[string]interface{})
		networks, _ := gateway["networkPackages"].(map[string]interface{})
		byNetwork, _ := networks[network].(map[string]interface{})
		byProduct, _ := byNetwork[u.req.Product].(map[string]interface{})
		if users, ok := byProduct["users"].(map[string]interface{}); ok {
			for i, c := range codes {
				if replacement, ok := users[c].(string); ok && replacement != "" {
					codes[i] = replacement
				}
			}
		}
	}
	if len(codes) == 0 || u.req.Packages == nil {
		return
	}
	var packages []*types.Package
	if len(codes) == 1 {
		p, err := u.req.Packages(codes[0])
		if err != nil {
			return
		}
		packages = append(packages, p)
	} else {
		for _, c := range codes {
			p, err := u.req.Packages(c)
			if err != nil {
				var ce *provision.CodeError
				if errors.As(err, &ce) && ce.Code == 201 {
					u.req.Logger.Error("unable to load all packages from provision: cannot find package", zap.String("package", c))
					continue
				}
				return
			}
			packages = append(packages, p)
		}
	}
	u.acl = MergeACL(packages)
}

func lastSeenNetwork(reg *types.RegistryData) string {
	if reg == nil {
		return ""
	}
	gateway, _ := reg.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	lastSeen, _ := value["lastSeen"].(map[string]interface{})
	network, _ := lastSeen["network"].(string)
	return network
}

// ID is the user id of the token (Node.js uracDriver.id), "" without one.
func (u *User) ID() string { return u.id }

// Record is the user record, nil before Init.
func (u *User) Record() map[string]interface{} { return u.record }

// TokenUser is the user of the token.
func (u *User) TokenUser() map[string]interface{} { return u.tokenUser }

// Profile is the user as the gateway gives it (getProfile); all adds config and groupsConfig.
// Nil without a record.
func (u *User) Profile(all bool) map[string]interface{} {
	if u.record == nil {
		return nil
	}
	if _, ok := u.record["username"].(string); !ok {
		return nil
	}
	keys := []string{"_id", "username", "firstName", "lastName", "email", "groups", "profile", "tenant"}
	if truthy(u.record["memberships"]) {
		keys = append(keys, "memberships")
	}
	if all {
		keys = append(keys, "config", "groupsConfig")
	}
	out := make(map[string]interface{}, len(keys))
	for _, k := range keys {
		if v, ok := u.record[k]; ok {
			out[k] = v
		}
	}
	return out
}

// ACL is the user's ACL in the gateway's environment, nil when the user has none.
func (u *User) ACL() map[string]interface{} {
	if u.acl == nil || u.acl.ACL == nil {
		return nil
	}
	return u.acl.ACL
}

// ACLAllEnv is the user's ACL by environment, nil when the user has none.
func (u *User) ACLAllEnv() map[string]interface{} {
	if u.acl == nil || u.acl.ACLAllEnv == nil {
		return nil
	}
	return u.acl.ACLAllEnv
}

// Config is the user's configuration for the request's key: config.keys.<iKey>.config.
func (u *User) Config() map[string]interface{} {
	if u.record == nil {
		return nil
	}
	config, _ := u.record["config"].(map[string]interface{})
	keys, _ := config["keys"].(map[string]interface{})
	key, _ := keys[u.req.IKey].(map[string]interface{})
	out, _ := key["config"].(map[string]interface{})
	return out
}

// Groups are the user's group codes, nil when the record has none.
func (u *User) Groups() []interface{} {
	if u.record == nil {
		return nil
	}
	groups, _ := u.record["groups"].([]interface{})
	return groups
}

// AllowedPackages are the packages the user's groups allow under the request's product.
func (u *User) AllowedPackages() []interface{} {
	if u.record == nil {
		return nil
	}
	groupsConfig, _ := u.record["groupsConfig"].(map[string]interface{})
	allowed, _ := groupsConfig["allowedPackages"].(map[string]interface{})
	list, _ := allowed[u.req.Product].([]interface{})
	return list
}
