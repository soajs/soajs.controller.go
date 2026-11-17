package types

// Package is a package as the Node.js provision gives it (getPackagesFromDb): its ACL for the
// gateway's environment, and for every environment.
type Package struct {
	Code string `json:"code"`
	// ACL is acl[<env>]; nil when the package has no ACL in the environment.
	ACL map[string]interface{} `json:"acl"`
	// ACLAllEnv is the whole acl, by environment.
	ACLAllEnv map[string]interface{} `json:"acl_all_env"`
	// TTL is how long the gateway caches the package, in ms.
	TTL int64 `json:"_TTL"`
	// Damage is what the gateway left out of the package because it could not read it
	// (docs/data-schema/imperfect-data.md). It is never injected.
	Damage *PackageDamage `json:"-"`
}

// PackageDamage is what was left out of a package's document. A user whose groups allow the
// package is refused what was left out, never given the key's package ACL instead.
type PackageDamage struct {
	// Whole: the package could not be read at all.
	Whole bool
	// Envs are the environments whose whole ACL was left out.
	Envs map[string]bool
	// Services are the services left out, by environment.
	Services map[string]map[string]bool
}

// Refuses reports whether the damage refuses everything in env.
func (d *PackageDamage) Refuses(env string) bool {
	return d != nil && (d.Whole || d.Envs[env])
}
