package types

// Key represents API key information
type Key struct {
	ExtKey      string                 `json:"extKey"`
	TenantCode  string                 `json:"tenantCode"`
	TenantID    string                 `json:"tenantId"`
	ProductCode string                 `json:"productCode"`
	PackageCode string                 `json:"packageCode"`
	ACL         map[string]*ServiceACL `json:"acl"`
	Config      map[string]interface{} `json:"config"`
}

// ServiceACL represents ACL for a service
type ServiceACL struct {
	Access      bool                `json:"access"`
	APIsPermission string           `json:"apisPermission"`
	OAuth       bool                `json:"oauth"`
	APIs        map[string]*APIACL  `json:"apis"`
	APIsRegExp  []*APIRegExpACL     `json:"apisRegExp"`
}

// APIACL represents ACL for a specific API
type APIACL struct {
	Access bool `json:"access"`
	OAuth  bool `json:"oauth"`
}

// APIRegExpACL represents ACL with regex pattern
type APIRegExpACL struct {
	RegExp string `json:"regExp"`
	Access bool   `json:"access"`
	OAuth  bool   `json:"oauth"`
}
