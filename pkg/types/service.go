package types

import "encoding/json"

// ServiceConfig represents a service configuration
type ServiceConfig struct {
	ID                    string                  `json:"_id" bson:"_id"`
	Name                  string                  `json:"name" bson:"name"`
	Group                 string                  `json:"group" bson:"group"`
	Version               string                  `json:"version" bson:"version"`
	Port                  int                     `json:"port" bson:"port"`
	Host                  string                  `json:"host" bson:"host"`
	RequestTimeout        int                     `json:"requestTimeout" bson:"requestTimeout"`
	RequestTimeoutRenewal int                     `json:"requestTimeoutRenewal" bson:"requestTimeoutRenewal"`
	Maintenance           *MaintenanceInfo        `json:"maintenance" bson:"maintenance"`
	Versions              map[string]*VersionInfo `json:"versions" bson:"versions"`
	// Hosts stores host IPs by version (like Node.js registry.services[name].hosts)
	Hosts *ServiceHosts `json:"hosts,omitempty"`
}

// ServiceHosts stores hosts for a service, organized by version
type ServiceHosts struct {
	// Latest holds the latest version string
	Latest string `json:"latest,omitempty"`
	// ByVersion maps version string to list of host IPs
	ByVersion map[string][]string `json:"-"`
}

// MarshalJSON implements custom JSON marshaling for ServiceHosts
// Output format matches Node.js: {"latest": "1", "1": ["127.0.0.1"]}
func (h ServiceHosts) MarshalJSON() ([]byte, error) {
	// Create a map to hold all fields
	result := make(map[string]interface{})

	if h.Latest != "" {
		result["latest"] = h.Latest
	}

	// Add version entries
	for version, ips := range h.ByVersion {
		result[version] = ips
	}

	return json.Marshal(result)
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
	APIs           []APIInfo          `json:"apis" bson:"apis"`
	ExtKeyRequired bool               `json:"extKeyRequired" bson:"extKeyRequired"`
	OAuth          bool               `json:"oauth" bson:"oauth"`
	InterConnect   []InterConnectItem `json:"interConnect" bson:"interConnect"`
}

// InterConnectItem defines a service that this service can connect to
type InterConnectItem struct {
	Name    string `json:"name" bson:"name"`
	Version string `json:"version" bson:"version"`
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
