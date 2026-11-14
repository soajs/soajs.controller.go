package types

import (
	"encoding/json"
	"sort"
)

// ServiceConfig is a registry service entry (registry.services.<name>). The controller entry
// also carries Name, Version and Authorization.
type ServiceConfig struct {
	Name                  string                  `json:"name,omitempty"`
	Group                 string                  `json:"group"`
	Version               string                  `json:"version,omitempty"`
	Authorization         *bool                   `json:"authorization,omitempty"`
	Port                  int                     `json:"port"`
	OPort                 int                     `json:"oport,omitempty"`
	RequestTimeoutRenewal int                     `json:"requestTimeoutRenewal"`
	RequestTimeout        int                     `json:"requestTimeout"`
	Versions              map[string]*VersionInfo `json:"versions,omitempty"`
	Maintenance           *RootMaintenance        `json:"maintenance,omitempty"`
	Hosts                 *ServiceHosts           `json:"hosts,omitempty"`
}

// DaemonConfig is a registry mdaemon entry (registry.daemons.<name>).
type DaemonConfig struct {
	Group       string                        `json:"group"`
	Port        int                           `json:"port"`
	OPort       int                           `json:"oport,omitempty"`
	Versions    map[string]*DaemonVersionInfo `json:"versions,omitempty"`
	Maintenance *RootMaintenance              `json:"maintenance,omitempty"`
	Hosts       *ServiceHosts                 `json:"hosts,omitempty"`
}

// DaemonVersionInfo is one version of an mdaemon.
type DaemonVersionInfo struct {
	Maintenance *MaintenanceInfo `json:"maintenance"`
}

// ServiceHosts holds a service's host IPs by version, and the latest version.
type ServiceHosts struct {
	Latest    string
	ByVersion map[string][]string
}

// MarshalJSON writes {"latest": "1", "1": ["10.0.0.1"]}, as the Node.js registry holds it.
func (h ServiceHosts) MarshalJSON() ([]byte, error) {
	result := make(map[string]interface{}, len(h.ByVersion)+1)
	result["latest"] = h.Latest
	versions := make([]string, 0, len(h.ByVersion))
	for v := range h.ByVersion {
		versions = append(versions, v)
	}
	sort.Strings(versions)
	for _, v := range versions {
		result[v] = h.ByVersion[v]
	}
	return json.Marshal(result)
}

// RootMaintenance is the service-level copy of the first version's maintenance.
type RootMaintenance struct {
	Readiness string   `json:"readiness"`
	Port      PortInfo `json:"port"`
}

// MaintenanceInfo is a version's maintenance declaration.
type MaintenanceInfo struct {
	Readiness string    `json:"readiness"`
	Port      PortInfo  `json:"port"`
	Commands  []Command `json:"commands,omitempty"`
}

// PortInfo says which port answers maintenance: "maintenance" (port + maintenanceInc) or
// "inherit" (the service port).
type PortInfo struct {
	Type string `json:"type"`
}

// Command is a maintenance command.
type Command struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Icon  string `json:"icon,omitempty"`
}

// VersionInfo is one version of a service.
type VersionInfo struct {
	ExtKeyRequired  bool               `json:"extKeyRequired"`
	Urac            bool               `json:"urac"`
	UracProfile     bool               `json:"urac_Profile"`
	UracACL         bool               `json:"urac_ACL"`
	UracConfig      bool               `json:"urac_Config"`
	UracGroupConfig bool               `json:"urac_GroupConfig"`
	TenantProfile   bool               `json:"tenant_Profile"`
	ProvisionACL    bool               `json:"provision_ACL"`
	OAuth           bool               `json:"oauth"`
	InterConnect    []InterConnectItem `json:"interConnect"`
	Maintenance     *MaintenanceInfo   `json:"maintenance"`
}

// InterConnectItem is a peer a service calls directly. An empty Version means the peer's latest.
type InterConnectItem struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// HostInfo holds host information
type HostInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}

// ServiceInfo holds service registration info
type ServiceInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
}
