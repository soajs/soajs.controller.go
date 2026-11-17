package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// ServiceConfig is a registry service entry (registry.services.<name>). The controller entry
// also carries Name, Version and Authorization.
type ServiceConfig struct {
	Name    string `json:"name,omitempty"`
	Group   string `json:"group"`
	Version string `json:"version,omitempty"`
	// ExtKeyRequired and OAuth are the latest version's, at the root as Node.js's registry gives
	// them (modules/registry/index.js:224-234): the gateway does not read them, the SDKs do.
	ExtKeyRequired        *bool                   `json:"extKeyRequired,omitempty"`
	OAuth                 *bool                   `json:"oauth,omitempty"`
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
	Readiness string   `json:"readiness"`
	Port      PortInfo `json:"port"`
	// Commands is kept as stored, an empty list included.
	Commands *[]Command `json:"commands,omitempty"`
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
//
// Present, when set, names the fields a registration gave (/register): the others are written out
// of the registry dump, as Node.js writes undefined ones, and read as false.
type VersionInfo struct {
	Present map[string]bool `json:"-"`
	// Raw are the flags whose marketplace value is truthy but not a boolean: Node.js keeps that
	// value in the registry (value || false), the gateway decides on the boolean.
	Raw             map[string]interface{} `json:"-"`
	ExtKeyRequired  bool                   `json:"extKeyRequired"`
	Urac            bool                   `json:"urac"`
	UracProfile     bool                   `json:"urac_Profile"`
	UracACL         bool                   `json:"urac_ACL"`
	UracConfig      bool                   `json:"urac_Config"`
	UracGroupConfig bool                   `json:"urac_GroupConfig"`
	TenantProfile   bool                   `json:"tenant_Profile"`
	ProvisionACL    bool                   `json:"provision_ACL"`
	OAuth           bool                   `json:"oauth"`
	InterConnect    []InterConnectItem     `json:"interConnect"`
	Maintenance     *MaintenanceInfo       `json:"maintenance"`
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

// MarshalJSON writes the version as Node.js's registry holds it: every field from the marketplace,
// only the given ones after a registration.
func (v VersionInfo) MarshalJSON() ([]byte, error) {
	fields := []struct {
		key   string
		value interface{}
	}{
		{"extKeyRequired", v.ExtKeyRequired}, {"urac", v.Urac}, {"urac_Profile", v.UracProfile}, {"urac_ACL", v.UracACL},
		{"urac_Config", v.UracConfig}, {"urac_GroupConfig", v.UracGroupConfig}, {"tenant_Profile", v.TenantProfile},
		{"provision_ACL", v.ProvisionACL}, {"oauth", v.OAuth}, {"interConnect", v.InterConnect}, {"maintenance", v.Maintenance},
	}
	var b bytes.Buffer
	b.WriteByte('{')
	first := true
	for _, f := range fields {
		if v.Present != nil && !v.Present[f.key] {
			continue
		}
		if !first {
			b.WriteByte(',')
		}
		first = false
		value := f.value
		if r, ok := v.Raw[f.key]; ok {
			value = r
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&b, "%q:", f.key)
		b.Write(raw)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
