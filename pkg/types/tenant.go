package types

// Tenant represents a SOAJS tenant
type Tenant struct {
	ID              string        `json:"_id" bson:"_id"`
	Code            string        `json:"code" bson:"code"`
	Name            string        `json:"name" bson:"name"`
	Description     string        `json:"description" bson:"description"`
	Type            string        `json:"type" bson:"type"`
	MaintenanceMode bool          `json:"maintenance" bson:"maintenance"`
	Profile         interface{}   `json:"profile" bson:"profile"`
	Applications    []Application `json:"applications" bson:"applications"`
}

// Application represents a tenant application
type Application struct {
	Product     string        `json:"product" bson:"product"`
	Package     string        `json:"package" bson:"package"`
	AppID       string        `json:"appId" bson:"appId"`
	Description string        `json:"description" bson:"description"`
	ACL         interface{}   `json:"acl" bson:"acl"`
	Keys        []ApplicationKey `json:"keys" bson:"keys"`
}

// ApplicationKey represents an application key
type ApplicationKey struct {
	Key     string   `json:"key" bson:"key"`
	ExtKeys []ExtKey `json:"extKeys" bson:"extKeys"`
	Config  interface{} `json:"config" bson:"config"`
}

// ExtKey represents an external key
type ExtKey struct {
	ExtKey     string                 `json:"extKey" bson:"extKey"`
	Device     interface{}            `json:"device" bson:"device"`
	Geo        interface{}            `json:"geo" bson:"geo"`
	Env        string                 `json:"env" bson:"env"`
	Expiration interface{}            `json:"expDate" bson:"expDate"`
	ACL        map[string]*ServiceACL `json:"acl" bson:"acl"`
	Config     map[string]interface{} `json:"config" bson:"config"`
}
