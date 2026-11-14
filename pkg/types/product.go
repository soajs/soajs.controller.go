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
}
