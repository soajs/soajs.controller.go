package types

// ServiceParams holds service-related request parameters
type ServiceParams struct {
	URLInfo       *URLInfo
	KeyACL        *ServiceACL
	FinalACL      *ServiceACL
	Product       *Product
	Package       *Package
	UserInfo      map[string]interface{}
	RequiresOAuth bool
}

// URLInfo holds parsed URL information
type URLInfo struct {
	OriginalPath   string
	ServiceName    string
	ServiceVersion string
	ServicePath    string
}

// DeviceInfo holds device information
type DeviceInfo struct {
	UserAgent string `json:"userAgent"`
	Type      string `json:"type"`
	OS        string `json:"os"`
	Browser   string `json:"browser"`
}

// GeoInfo holds geographic information
type GeoInfo struct {
	Country   string  `json:"country"`
	City      string  `json:"city"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

// AccessToken represents an OAuth access token
type AccessToken struct {
	Token     string                 `json:"accessToken" bson:"token"`
	ClientID  string                 `json:"clientId" bson:"clientId"`
	UserID    string                 `json:"userId" bson:"userId"`
	Expires   int64                  `json:"accessTokenExpiresAt" bson:"expires"`
	Scope     string                 `json:"scope" bson:"scope"`
	User      map[string]interface{} `json:"user" bson:"user"`
}

// RefreshToken represents an OAuth refresh token
type RefreshToken struct {
	Token    string `json:"refreshToken" bson:"token"`
	ClientID string `json:"clientId" bson:"clientId"`
	UserID   string `json:"userId" bson:"userId"`
	Expires  int64  `json:"refreshTokenExpiresAt" bson:"expires"`
	Scope    string `json:"scope" bson:"scope"`
}
