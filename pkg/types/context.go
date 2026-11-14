package types

import "time"

// ServiceParams holds service-related request parameters
type ServiceParams struct {
	URLInfo        *URLInfo
	Package        *Package
	ACL            *FinalACL    // the final ACL the keyACL middleware builds
	TenantOauth    *TenantOauth // the tenant's oauth settings
	IsAPIPublic    bool         // the API needs no logged-in user
	KeyAPI         bool         // the API is called with a key (extKeyRequired): PIN rules apply
	AccessToken    string       // access_token from the query string, else its header (Node.js req.query.access_token)
	HasAccessToken bool         // AccessToken was given
	RequiresOAuth  bool
	OAuthToken     *AccessToken
	Environment    string // Current environment
	ServiceName    string // Target service name
	ServiceVersion string // Target service version
	SkipACL        bool   // Skip ACL checks (MT whitelist)
	SkipOAuth      bool   // Skip OAuth checks (MT whitelist)
	// GatewayAPI is /soajs/acl, an API of the gateway itself (Node.js mimicGatewayApi): mt runs
	// the key, OAuth and user checks only, and the answer is made from the caller's ACL.
	GatewayAPI bool
	// Proxy is /soajs/proxy: mt skips the ACL checks and always asks for a token.
	Proxy bool
}

// URLInfo holds parsed URL information
type URLInfo struct {
	// URL is the raw path after the service, with its query string: what the service is called
	// with (Node.js serviceParams.url).
	URL string
	// FullPath is the path, with the service and without the version segment (Node.js
	// parsedUrl.pathname).
	FullPath         string
	OriginalPath     string
	ServiceName      string
	ServiceVersion   string
	ServicePath      string
	VersionRequested bool // True if version was explicitly requested in URL
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

// AccessToken is an oauth_token document of type accessToken (docs/data-schema/oauth_token.md).
type AccessToken struct {
	Token    string                 `json:"token"`
	Type     string                 `json:"type"`
	ClientID string                 `json:"clientId"`
	Env      string                 `json:"env"`
	Expires  time.Time              `json:"expires"`
	User     map[string]interface{} `json:"user"`
}
