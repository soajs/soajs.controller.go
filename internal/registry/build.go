package registry

import (
	"fmt"
	"strings"

	"github.com/soajs/soajs.controller.go/internal/dbconfig"
	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// gatewayName is the name of the gateway's registry entry, as in Node.js.
const gatewayName = "controller"

// envDoc is an environment document (docs/data-schema/environment.md).
type envDoc struct {
	Code       string                 `json:"code"`
	Domain     string                 `json:"domain"`
	APIPrefix  string                 `json:"apiPrefix"`
	SitePrefix string                 `json:"sitePrefix"`
	Protocol   string                 `json:"protocol"`
	Port       int                    `json:"port"`
	Deployer   map[string]interface{} `json:"deployer"`
	Dbs        struct {
		Config struct {
			Prefix string `json:"prefix"`
		} `json:"config"`
		Databases map[string]struct {
			Cluster        string `json:"cluster"`
			TenantSpecific bool   `json:"tenantSpecific"`
			Prefix         string `json:"prefix"`
		} `json:"databases"`
	} `json:"dbs"`
	Services struct {
		Controller struct {
			Authorization         bool `json:"authorization"`
			RequestTimeout        int  `json:"requestTimeout"`
			RequestTimeoutRenewal int  `json:"requestTimeoutRenewal"`
		} `json:"controller"`
		Config types.GlobalServiceConfig `json:"config"`
	} `json:"services"`
}

// clusterConfig is the config of a resource of type cluster (docs/data-schema/resources.md).
type clusterConfig struct {
	Protocol    string                 `json:"protocol"`
	Servers     []dbconfig.Server      `json:"servers"`
	Credentials *dbconfig.Credentials  `json:"credentials"`
	URLParam    map[string]interface{} `json:"URLParam"`
	Streaming   map[string]interface{} `json:"streaming"`
}

// marketItem is a marketplace service or mdaemon (docs/data-schema/marketplace.md).
type marketItem struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Configuration struct {
		Group                 string `json:"group"`
		Port                  int    `json:"port"`
		RequestTimeout        int    `json:"requestTimeout"`
		RequestTimeoutRenewal int    `json:"requestTimeoutRenewal"`
	} `json:"configuration"`
	Versions []marketVersion `json:"versions"`
}

type marketVersion struct {
	Version string `json:"version"`
	types.VersionInfo
}

// buildDatabases builds coreDB and tenantMetaDB from the environment's dbs and its cluster
// resources (Node.js modules/registry/index.js build.metaAndCoreDB, without dbs.clusters and
// inline servers). A database whose cluster is not a resource of the environment is an error.
func buildDatabases(env *envDoc, clusters map[string]map[string]interface{}, envCode string, timeLoaded int64) (core, meta map[string]*dbconfig.DB, err error) {
	core = map[string]*dbconfig.DB{}
	meta = map[string]*dbconfig.DB{}
	for name, rec := range env.Dbs.Databases {
		resource, ok := clusters[rec.Cluster]
		if !ok {
			return nil, nil, fmt.Errorf("database %s: cluster %s is not a resource of this environment", name, rec.Cluster)
		}
		var cfg clusterConfig
		if err := remarshal(resource["config"], &cfg); err != nil {
			return nil, nil, fmt.Errorf("database %s: cluster %s: %w", name, rec.Cluster, err)
		}
		prefix := env.Dbs.Config.Prefix
		if rec.Prefix != "" {
			prefix = rec.Prefix
		}
		db := &dbconfig.DB{
			Prefix:      prefix,
			Cluster:     rec.Cluster,
			Protocol:    cfg.Protocol,
			Servers:     cfg.Servers,
			Credentials: cfg.Credentials,
			URLParam:    cfg.URLParam,
			Streaming:   cfg.Streaming,
		}
		if rec.TenantSpecific {
			db.Name = "#TENANT_NAME#_" + name
			db.RegistryLocation = &dbconfig.Location{L1: "metaDB", L2: db.Name, Env: envCode, Cluster: rec.Cluster, TimeLoaded: timeLoaded}
			meta[name] = db
		} else {
			db.Name = name
			db.RegistryLocation = &dbconfig.Location{L1: "coreDB", L2: name, Env: envCode, Cluster: rec.Cluster, TimeLoaded: timeLoaded}
			core[name] = db
		}
	}
	return core, meta, nil
}

// controllerEntry is registry.services.controller (modules/registry/index.js:364-375).
func controllerEntry(env *envDoc) *types.ServiceConfig {
	authorization := env.Services.Controller.Authorization
	timeout := env.Services.Controller.RequestTimeout
	if timeout == 0 {
		timeout = 30
	}
	return &types.ServiceConfig{
		Name:                  gatewayName,
		Group:                 "Gateway",
		Version:               "1",
		Authorization:         &authorization,
		Port:                  env.Services.Config.Ports.Controller,
		RequestTimeout:        timeout,
		RequestTimeoutRenewal: env.Services.Controller.RequestTimeoutRenewal,
	}
}

// addService adds a marketplace service (build.allServices, without the root version,
// extKeyRequired and oauth that only endpoint services used).
func addService(services map[string]*types.ServiceConfig, item *marketItem) {
	if item.Name == gatewayName {
		return
	}
	svc := &types.ServiceConfig{
		Group:                 item.Configuration.Group,
		Port:                  item.Configuration.Port,
		RequestTimeout:        item.Configuration.RequestTimeout,
		RequestTimeoutRenewal: item.Configuration.RequestTimeoutRenewal,
		Versions:              map[string]*types.VersionInfo{},
	}
	for i := range item.Versions {
		v := item.Versions[i]
		info := v.VersionInfo
		svc.Versions[v.Version] = &info
		if svc.Maintenance == nil && info.Maintenance != nil {
			svc.Maintenance = &types.RootMaintenance{Readiness: info.Maintenance.Readiness, Port: info.Maintenance.Port}
		}
	}
	services[item.Name] = svc
}

// addDaemon adds a marketplace mdaemon (build.allDaemons).
func addDaemon(daemons map[string]*types.DaemonConfig, item *marketItem) {
	if item.Name == gatewayName {
		return
	}
	d := &types.DaemonConfig{
		Group:    item.Configuration.Group,
		Port:     item.Configuration.Port,
		Versions: map[string]*types.DaemonVersionInfo{},
	}
	for i := range item.Versions {
		v := item.Versions[i]
		d.Versions[v.Version] = &types.DaemonVersionInfo{Maintenance: v.Maintenance}
		if d.Maintenance == nil && v.Maintenance != nil {
			d.Maintenance = &types.RootMaintenance{Readiness: v.Maintenance.Readiness, Port: v.Maintenance.Port}
		}
	}
	daemons[item.Name] = d
}

// addHost records a host under a service or mdaemon (build.servicesHosts): its IP under its
// version, the latest version, and, unless the environment is sensitive, the host's port in place
// of the service port (the previous one kept as oport).
func addHost(hosts **types.ServiceHosts, port, oport *int, h *types.HostRecord) {
	if !IsSensitiveEnv(h.Env) && h.Port != 0 {
		*oport = *port
		*port = h.Port
	}
	if *hosts == nil {
		*hosts = &types.ServiceHosts{Latest: h.Version, ByVersion: map[string][]string{}}
	}
	if lib.IsLatest(h.Version, (*hosts).Latest) {
		(*hosts).Latest = h.Version
	}
	for _, ip := range (*hosts).ByVersion[h.Version] {
		if ip == h.IP {
			return
		}
	}
	(*hosts).ByVersion[h.Version] = append((*hosts).ByVersion[h.Version], h.IP)
}

// applyHosts adds the environment's hosts to services, mdaemons and the gateway entry.
func applyHosts(reg *types.RegistryData, hosts []*types.HostRecord) {
	for _, h := range hosts {
		if h.Name == gatewayName {
			gw := reg.Services[gatewayName]
			if gw.Hosts == nil {
				gw.Hosts = &types.ServiceHosts{Latest: h.Version, ByVersion: map[string][]string{}}
			}
			if lib.IsLatest(h.Version, gw.Hosts.Latest) {
				gw.Hosts.Latest = h.Version
			}
			if !contains(gw.Hosts.ByVersion[h.Version], h.IP) {
				gw.Hosts.ByVersion[h.Version] = append(gw.Hosts.ByVersion[h.Version], h.IP)
			}
			continue
		}
		if svc, ok := reg.Services[h.Name]; ok {
			addHost(&svc.Hosts, &svc.Port, &svc.OPort, h)
		}
		if d, ok := reg.Daemons[h.Name]; ok {
			addHost(&d.Hosts, &d.Port, &d.OPort, h)
		}
	}
}

// available reports whether a plugged resource or custom entry belongs to the environment: it was
// created in it, or is shared to it.
func available(doc map[string]interface{}, envUpper string) bool {
	if created, _ := doc["created"].(string); created == envUpper {
		return true
	}
	if doc["shared"] != true {
		return false
	}
	shared, _ := doc["sharedEnvs"].(map[string]interface{})
	return shared[envUpper] == true
}

func contains(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func upper(s string) string { return strings.ToUpper(s) }
