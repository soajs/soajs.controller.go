package types

// Product represents a SOAJS product
type Product struct {
	ID          string     `json:"_id" bson:"_id"`
	Code        string     `json:"code" bson:"code"`
	Name        string     `json:"name" bson:"name"`
	Description string     `json:"description" bson:"description"`
	Packages    []*Package `json:"packages" bson:"packages"`
}

// Package represents a product package
type Package struct {
	Code        string                 `json:"code" bson:"code"`
	Name        string                 `json:"name" bson:"name"`
	Description string                 `json:"description" bson:"description"`
	ACL         map[string]interface{} `json:"acl" bson:"acl"`                     // Nested: ACL[env][service][version]
	ACLAllEnv   map[string]interface{} `json:"acl_all_env" bson:"acl_all_env"`     // ACL for all environments
	TTL         int64                  `json:"_TTL" bson:"_TTL"`
}
