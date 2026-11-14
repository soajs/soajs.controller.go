package types

// Tenant is the request's tenant, from its key.
type Tenant struct {
	ID      string                 `json:"id"`
	Code    string                 `json:"code"`
	Name    string                 `json:"name"`
	Type    string                 `json:"type"`
	Locked  bool                   `json:"locked"`
	Profile map[string]interface{} `json:"profile"`
	Main    *TenantRef             `json:"main,omitempty"`
}

// TenantFromKey is the tenant of a key.
func TenantFromKey(k *Key) *Tenant {
	return &Tenant{
		ID:      k.Tenant.ID,
		Code:    k.Tenant.Code,
		Name:    k.Tenant.Name,
		Type:    k.Tenant.Type,
		Locked:  k.Tenant.Locked,
		Profile: k.Tenant.Profile,
		Main:    k.Tenant.Main,
	}
}
