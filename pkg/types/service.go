package types

// ServiceConfig represents a service configuration
type ServiceConfig struct {
	ID                string            `json:"_id" bson:"_id"`
	Name              string            `json:"name" bson:"name"`
	Group             string            `json:"group" bson:"group"`
	Version           string            `json:"version" bson:"version"`
	Port              int               `json:"port" bson:"port"`
	Host              string            `json:"host" bson:"host"`
	RequestTimeout    int               `json:"requestTimeout" bson:"requestTimeout"`
	RequestTimeoutRenewal int           `json:"requestTimeoutRenewal" bson:"requestTimeoutRenewal"`
	Maintenance       *MaintenanceInfo  `json:"maintenance" bson:"maintenance"`
	Versions          map[string]*VersionInfo `json:"versions" bson:"versions"`
}

// MaintenanceInfo holds maintenance configuration
type MaintenanceInfo struct {
	Readiness string   `json:"readiness" bson:"readiness"`
	Port      PortInfo `json:"port" bson:"port"`
	Commands  []Command `json:"commands" bson:"commands"`
}

// PortInfo holds port configuration
type PortInfo struct {
	Type string `json:"type" bson:"type"`
}

// Command represents a maintenance command
type Command struct {
	Label string `json:"label" bson:"label"`
	Path  string `json:"path" bson:"path"`
	Icon  string `json:"icon" bson:"icon"`
}

// VersionInfo holds version-specific configuration
type VersionInfo struct {
	APIs      []APIInfo `json:"apis" bson:"apis"`
	ExtKeyRequired bool  `json:"extKeyRequired" bson:"extKeyRequired"`
	OAuth     bool      `json:"oauth" bson:"oauth"`
}

// APIInfo holds API endpoint information
type APIInfo struct {
	Route  string `json:"l" bson:"l"`
	Method string `json:"m" bson:"m"`
	Group  string `json:"group" bson:"group"`
}

// DaemonConfig represents a daemon configuration
type DaemonConfig struct {
	ID      string `json:"_id" bson:"_id"`
	Name    string `json:"name" bson:"name"`
	Group   string `json:"group" bson:"group"`
	Version string `json:"version" bson:"version"`
	Port    int    `json:"port" bson:"port"`
}

// ServiceInfo holds service registration info
type ServiceInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// HostInfo holds host information
type HostInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}
