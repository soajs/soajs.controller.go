package model

// Group represents a URAC group
type Group struct {
	ID          string      `json:"_id" bson:"_id"`
	Code        string      `json:"code" bson:"code"`
	Name        string      `json:"name" bson:"name"`
	Description string      `json:"description" bson:"description"`
	Tenant      TenantRef   `json:"tenant" bson:"tenant"`
	Config      interface{} `json:"config" bson:"config"`
}
