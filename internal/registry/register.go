package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// RegisterOptions is a registration on the maintenance port (server/maintenance.js /register): the
// fields a service sent, a flag left out being nil.
type RegisterOptions struct {
	Name, Group, IP, Type, Version, SubType, Description string
	Port, PortHost                                       int
	RequestTimeout, RequestTimeoutRenewal                int
	// Flags are the version flags the registration gave (extKeyRequired, oauth, urac, ...).
	Flags        map[string]bool
	InterConnect []types.InterConnectItem
	HasInterConn bool
	Maintenance  *types.MaintenanceInfo
	APIList      interface{}
	MW           bool
}

// Register records a registration in the registry as Node.js does (modules/registry/index.js
// register): a service or daemon the registry does not have is added with its group and port; its
// port follows the registration; the version gets the maintenance declaration and, for a service,
// the given flags (extKeyRequired false when not given), which become the root's; the IP joins the
// version's hosts. With mw, the host and the marketplace item are written too. It returns the
// entry as the registry now holds it.
func (r *MongoRegistry) Register(opt RegisterOptions) (interface{}, error) {
	if opt.IP == "" || opt.Name == "" {
		return nil, errors.New("unable to register service. missing ip or name param")
	}
	if _, ok := opt.Flags["extKeyRequired"]; !ok {
		if opt.Flags == nil {
			opt.Flags = map[string]bool{}
		}
		opt.Flags["extKeyRequired"] = false
	}
	r.mu.Lock()
	if r.data == nil {
		r.mu.Unlock()
		return nil, errors.New("registry not loaded")
	}
	// copy on write: readers keep the registry they hold
	data := *r.data
	var entry interface{}
	if opt.Type == "service" {
		services := make(map[string]*types.ServiceConfig, len(data.Services)+1)
		for k, v := range data.Services {
			services[k] = v
		}
		var svc *types.ServiceConfig
		if cur := services[opt.Name]; cur != nil {
			c := *cur
			svc = &c
		} else {
			if opt.Port == 0 {
				r.mu.Unlock()
				return nil, errors.New("unable to register service. missing port param")
			}
			svc = &types.ServiceConfig{Group: opt.Group, Port: opt.Port, RequestTimeout: opt.RequestTimeout, RequestTimeoutRenewal: opt.RequestTimeoutRenewal}
		}
		if svc.Port != opt.Port {
			svc.Port = opt.Port
		}
		versions := make(map[string]*types.VersionInfo, len(svc.Versions)+1)
		for k, v := range svc.Versions {
			versions[k] = v
		}
		v := &types.VersionInfo{Present: map[string]bool{}}
		if cur := versions[opt.Version]; cur != nil && cur.Maintenance != nil {
			v.Maintenance = cur.Maintenance
			v.Present["maintenance"] = true
		}
		if opt.Maintenance != nil {
			v.Maintenance = opt.Maintenance
			v.Present["maintenance"] = true
		}
		setFlags(v, opt.Flags)
		if opt.HasInterConn {
			v.InterConnect = opt.InterConnect
			v.Present["interConnect"] = true
		}
		versions[opt.Version] = v
		svc.Versions = versions
		extKey := v.ExtKeyRequired
		svc.ExtKeyRequired, svc.Version = &extKey, opt.Version
		svc.Hosts = withHost(svc.Hosts, opt.Version, opt.IP)
		if opt.MW && opt.PortHost != 0 {
			svc.Port = opt.PortHost
		}
		services[opt.Name] = svc
		data.Services = services
		entry = svc
	} else {
		daemons := make(map[string]*types.DaemonConfig, len(data.Daemons)+1)
		for k, v := range data.Daemons {
			daemons[k] = v
		}
		var d *types.DaemonConfig
		if cur := daemons[opt.Name]; cur != nil {
			c := *cur
			d = &c
		} else {
			if opt.Port == 0 {
				r.mu.Unlock()
				return nil, errors.New("unable to register service. missing port param")
			}
			d = &types.DaemonConfig{Group: opt.Group, Port: opt.Port}
		}
		if d.Port != opt.Port {
			d.Port = opt.Port
		}
		versions := make(map[string]*types.DaemonVersionInfo, len(d.Versions)+1)
		for k, v := range d.Versions {
			versions[k] = v
		}
		dv := &types.DaemonVersionInfo{}
		if cur := versions[opt.Version]; cur != nil {
			c := *cur
			dv = &c
		}
		if opt.Maintenance != nil {
			dv.Maintenance = opt.Maintenance
		}
		versions[opt.Version] = dv
		d.Versions = versions
		d.Hosts = withHost(d.Hosts, opt.Version, opt.IP)
		if opt.MW && opt.PortHost != 0 {
			d.Port = opt.PortHost
		}
		daemons[opt.Name] = d
		data.Daemons = daemons
		entry = d
	}
	data.TimeLoaded = time.Now().UnixMilli()
	r.data = &data
	r.mu.Unlock()

	if opt.MW {
		hostname, _ := os.Hostname()
		port := opt.Port
		if opt.PortHost != 0 {
			port = opt.PortHost
		}
		_ = r.RegisterHost(types.HostRecord{Env: r.GetEnvironment(), Name: opt.Name, Version: opt.Version, IP: opt.IP, Port: port, Hostname: strings.ToLower(hostname), Type: opt.Type})
		if err := r.registerNewService(opt); err != nil {
			return nil, err
		}
	}
	return entry, nil
}

var flagFields = []string{"extKeyRequired", "oauth", "urac", "urac_Profile", "urac_ACL", "urac_Config", "urac_GroupConfig", "tenant_Profile", "provision_ACL"}

func setFlags(v *types.VersionInfo, flags map[string]bool) {
	for _, f := range flagFields {
		val, ok := flags[f]
		if !ok {
			continue
		}
		v.Present[f] = true
		switch f {
		case "extKeyRequired":
			v.ExtKeyRequired = val
		case "oauth":
			v.OAuth = val
		case "urac":
			v.Urac = val
		case "urac_Profile":
			v.UracProfile = val
		case "urac_ACL":
			v.UracACL = val
		case "urac_Config":
			v.UracConfig = val
		case "urac_GroupConfig":
			v.UracGroupConfig = val
		case "tenant_Profile":
			v.TenantProfile = val
		case "provision_ACL":
			v.ProvisionACL = val
		}
	}
}

func withHost(hosts *types.ServiceHosts, version, ip string) *types.ServiceHosts {
	h := &types.ServiceHosts{ByVersion: map[string][]string{}}
	if hosts != nil {
		h.Latest = hosts.Latest
		for k, v := range hosts.ByVersion {
			h.ByVersion[k] = append([]string(nil), v...)
		}
	} else {
		h.Latest = version
	}
	if !containsIP(h.ByVersion[version], ip) {
		h.ByVersion[version] = append(h.ByVersion[version], ip)
	}
	return h
}

func containsIP(list []string, ip string) bool {
	for _, e := range list {
		if e == ip {
			return true
		}
	}
	return false
}

// registerNewService upserts the marketplace item of a registration with mw (modules/registry/
// mongo.js registerNewService): the item of the same type on the same port is updated when it is
// the service's (its configuration, and the version updated or added), refused when another
// service has the port; without one, the item is inserted. A field the registration did not give
// is not written, where Node.js writes null.
func (r *MongoRegistry) registerNewService(opt RegisterOptions) error {
	if r.isHA {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	coll := r.db.Collection(CollMarketplace)
	configuration := bson.M{"group": opt.Group, "port": opt.Port}
	if opt.SubType != "" {
		configuration["subType"] = opt.SubType
	}
	version := bson.M{"version": opt.Version}
	if opt.Type == "service" {
		if opt.RequestTimeout != 0 {
			configuration["requestTimeout"] = opt.RequestTimeout
		}
		if opt.RequestTimeoutRenewal != 0 {
			configuration["requestTimeoutRenewal"] = opt.RequestTimeoutRenewal
		}
		for f, v := range opt.Flags {
			version[f] = v
		}
		if opt.HasInterConn {
			version["interConnect"] = opt.InterConnect
		}
		if opt.APIList != nil {
			version["apis"] = opt.APIList
		}
	}
	if opt.Maintenance != nil {
		version["maintenance"] = opt.Maintenance
	}
	var record struct {
		ID       interface{}              `bson:"_id"`
		Name     string                   `bson:"name"`
		Versions []map[string]interface{} `bson:"versions"`
	}
	err := coll.FindOne(ctx, bson.M{"type": opt.Type, "configuration.port": opt.Port}).Decode(&record)
	if err == drv.ErrNoDocuments {
		doc := bson.M{"type": opt.Type, "name": opt.Name, "configuration": configuration, "versions": []bson.M{version}}
		if opt.Description != "" {
			doc["description"] = opt.Description
		}
		_, err := coll.InsertOne(ctx, doc)
		return err
	}
	if err != nil {
		return err
	}
	if record.Name != opt.Name {
		return fmt.Errorf("Item of type [%s] with port [%d] is taken by [%s].", opt.Type, opt.Port, record.Name)
	}
	set := bson.M{}
	if opt.Description != "" {
		set["description"] = opt.Description
	}
	for k, v := range configuration {
		set["configuration."+k] = v
	}
	update := bson.M{"$set": set}
	opts := options.Update()
	found := false
	for _, v := range record.Versions {
		if v["version"] == opt.Version {
			found = true
		}
	}
	switch {
	case len(record.Versions) == 0:
		set["versions"] = []bson.M{version}
	case found:
		for k, v := range version {
			set["versions.$[elem]."+k] = v
		}
		opts.SetArrayFilters(options.ArrayFilters{Filters: []interface{}{bson.M{"elem.version": opt.Version}}})
	default:
		update["$push"] = bson.M{"versions": version}
	}
	_, err = coll.UpdateOne(ctx, bson.M{"_id": record.ID}, update, opts)
	return err
}

// ParseRegisterFlag reads a flag of a registration: a JSON boolean, or "true"/"false" from a query.
func ParseRegisterFlag(v interface{}) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		if b, err := strconv.ParseBool(t); err == nil && (t == "true" || t == "false") {
			return b, true
		}
	}
	return false, false
}

// DecodeMaintenance reads a registration's maintenance declaration.
func DecodeMaintenance(v interface{}) *types.MaintenanceInfo {
	if v == nil {
		return nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m types.MaintenanceInfo
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return &m
}
