package types

// Key is what an external key resolves to, as the Node.js provision gives it (soajs.provision
// getKeyData): the internal key, its tenant and application, the extKey's restrictions and the
// key's configuration for the gateway's environment.
type Key struct {
	InternalKey string                 `json:"key"`
	ExtKey      string                 `json:"extKey"`
	Tenant      KeyTenant              `json:"tenant"`
	Application KeyApplication         `json:"application"`
	Device      map[string]interface{} `json:"device"`
	Env         string                 `json:"env"`
	Geo         map[string]interface{} `json:"geo"`
	// Config is keys[].config[<env>], {} when the key has none for the environment.
	Config map[string]interface{} `json:"config"`
}

// KeyTenant is the tenant a key belongs to.
type KeyTenant struct {
	Name    string                 `json:"name"`
	ID      string                 `json:"id"`
	Code    string                 `json:"code"`
	Locked  bool                   `json:"locked"`
	Type    string                 `json:"type"`
	Profile map[string]interface{} `json:"profile"`
	// Main is the main tenant of a sub-tenant (type client).
	Main *TenantRef `json:"main,omitempty"`
}

// TenantRef names a tenant.
type TenantRef struct {
	ID   string `json:"id"`
	Code string `json:"code"`
}

// KeyApplication is the application a key belongs to.
type KeyApplication struct {
	Product string `json:"product"`
	Package string `json:"package"`
	AppID   string `json:"appId"`
}

// TenantOauth is a tenant's oauth settings (tenants.oauth).
// Only what the gateway reads is decoded: the secret, type and loginMode are the oauth service's.
type TenantOauth struct {
	Disabled int                       `json:"disabled"`
	Pin      map[string]TenantOauthPin `json:"pin,omitempty"`
}

// TenantOauthPin turns the PIN on for a product.
type TenantOauthPin struct {
	Enabled bool `json:"enabled"`
}

// ServiceACL represents ACL for a service
type ServiceACL struct {
	Access         bool               `json:"access"`
	APIsPermission string             `json:"apisPermission"`
	OAuth          bool               `json:"oauth"`
	APIs           map[string]*APIACL `json:"apis"`
	APIsRegExp     []*APIRegExpACL    `json:"apisRegExp"`
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
