package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/version"
	"go.uber.org/zap"
)

// maintenanceResponse is the answer of the maintenance port (server/maintenance.js
// maintenanceResponse): result, ts, the gateway as the service and the route, then data.
type maintenanceResponse struct {
	Result  bool        `json:"result"`
	TS      int64       `json:"ts"`
	Service serviceInfo `json:"service"`
	Data    interface{} `json:"data,omitempty"`
}

type serviceInfo struct {
	Service string `json:"service"`
	Version string `json:"version"`
	Type    string `json:"type"`
	Route   string `json:"route"`
}

// NewMaintenanceServer is the maintenance port, as the Node.js gateway's (server/maintenance.js):
// /favicon.ico, /reloadRegistry, /awarenessStat, /register, /loadProvision, /getRegistry, and the
// heartbeat on any other path. /cache/invalidate comes with the cache; /proxySocket is not
// ported (D7).
func NewMaintenanceServer(port int, ctrl *Controller, serviceIP string, logger *zap.Logger) *http.Server {
	h := &maintenanceHandler{ctrl: ctrl, logger: logger, serviceIP: serviceIP}
	return &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           h,
		ReadHeaderTimeout: 60 * time.Second,
	}
}

type maintenanceHandler struct {
	ctrl      *Controller
	logger    *zap.Logger
	serviceIP string
}

func (h *maintenanceHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.RequestURI == "/favicon.ico" {
		w.Header().Set("Content-Type", "image/x-icon")
		w.WriteHeader(http.StatusOK)
		return
	}
	pathname, search, err := lib.RequestURL(r.RequestURI)
	if err != nil {
		pathname, search = r.URL.Path, ""
		if r.URL.RawQuery != "" {
			search = "?" + r.URL.RawQuery
		}
	}
	query := map[string]string{}
	if values, err := url.ParseQuery(trimQuestion(search)); err == nil {
		for k, v := range values {
			query[k] = v[len(v)-1]
		}
	}
	switch pathname {
	case "/reloadRegistry":
		h.write(w, h.reloadRegistry(pathname), true)
	case "/awarenessStat":
		h.awarenessStat(w, pathname, query)
	case "/register":
		h.register(w, r, pathname, query)
	case "/loadProvision":
		resp := h.response(pathname)
		resp.Result = h.ctrl.ReloadProvision() == nil
		// Node.js ends this answer without writing its headers: no Content-Type
		h.write(w, resp, false)
	case "/getRegistry":
		h.getRegistry(w, pathname, query)
	default:
		resp := h.response(pathname)
		resp.Result = true
		h.write(w, resp, true)
	}
}

func trimQuestion(s string) string {
	if len(s) > 0 && s[0] == '?' {
		return s[1:]
	}
	return s
}

func (h *maintenanceHandler) response(route string) maintenanceResponse {
	return maintenanceResponse{
		TS:      time.Now().UnixMilli(),
		Service: serviceInfo{Service: "soajs.controller", Version: version.Get(), Type: "rest", Route: route},
	}
}

// write answers 200 with the response as JSON.stringify writes it.
func (h *maintenanceHandler) write(w http.ResponseWriter, resp maintenanceResponse, contentType bool) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
	if contentType {
		w.Header().Set("Content-Type", "application/json")
	} else {
		w.Header()["Content-Type"] = nil
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(bytes.TrimRight(b.Bytes(), "\n"))
}

func (h *maintenanceHandler) reloadRegistry(route string) maintenanceResponse {
	resp := h.response(route)
	if err := h.ctrl.ReloadRegistry(); err != nil {
		h.logger.Error("Failed to load registry. reusing from previous load. Reason: " + err.Error())
		return resp
	}
	resp.Result = true
	resp.Data = cloneAndFilterRegistry(h.ctrl.Registry().Get(), "", "")
	return resp
}

// awarenessStat answers the registry's services and daemons with their hosts' health; with
// ?update, outside HA, the gateway's controllers document is written too.
func (h *maintenanceHandler) awarenessStat(w http.ResponseWriter, route string, query map[string]string) {
	resp := h.response(route)
	reg := h.ctrl.Registry().Get()
	if reg != nil {
		services, daemons := awareness.RegistryWithStats(reg, h.ctrl.Awareness().Stats())
		resp.Result = true
		resp.Data = map[string]interface{}{"services": services, "daemons": daemons}
	}
	if os.Getenv("SOAJS_DEPLOY_HA") == "" && query["update"] != "" && reg != nil {
		data := awareness.ControllerData(reg, h.ctrl.Awareness().Stats(), h.ctrl.Gateway().Name, h.serviceIP, false, time.Now())
		go func(ts int64) {
			if err := h.ctrl.Registry().UpsertEnvController(h.ctrl.Registry().GetEnvironment(), awareness.ControllerIP(h.serviceIP), ts, data); err != nil {
				h.logger.Error(err.Error())
			}
		}(resp.TS)
	}
	h.write(w, resp, true)
}

// register records a service on its registration (outside HA): from the query, or the POST body.
// With ?serviceHATask the registry is reloaded instead. A version that is not one answers false.
func (h *maintenanceHandler) register(w http.ResponseWriter, r *http.Request, route string, query map[string]string) {
	resp := h.response(route)
	if os.Getenv("SOAJS_DEPLOY_HA") != "" {
		h.write(w, resp, true)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]interface{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil {
			h.logger.Error("Failed to register service: the body is not JSON")
			h.write(w, resp, true)
			return
		}
	}
	if query["serviceHATask"] != "" {
		h.write(w, h.reloadRegistry(route), true)
		return
	}
	info := map[string]interface{}{}
	for k, v := range query {
		info[k] = v
	}
	if r.Method == http.MethodPost && body != nil {
		info = body
	}
	ver := asString(info["version"])
	if !validVersion(ver) {
		h.logger.Error("Failed to register service for [" + asString(info["name"]) + "@" + ver + "] version should be of format [1.1]")
		h.write(w, resp, true)
		return
	}
	opt := registry.RegisterOptions{
		Name: asString(info["name"]), Group: asString(info["group"]), IP: asString(info["ip"]), Type: asString(info["type"]),
		Version: ver, SubType: asString(info["subType"]), Description: asString(info["description"]),
		Port: asInt(info["port"]), PortHost: asInt(info["portHost"]), MW: truthyValue(info["mw"]),
		Maintenance: registry.DecodeMaintenance(bodyValue(body, "maintenance")),
	}
	if opt.Type == "service" {
		opt.Flags = map[string]bool{}
		for _, f := range []string{"extKeyRequired", "oauth", "urac", "urac_Profile", "urac_ACL", "urac_Config", "urac_GroupConfig", "tenant_Profile", "provision_ACL"} {
			if b, ok := registry.ParseRegisterFlag(info[f]); ok {
				opt.Flags[f] = b
			}
		}
		opt.RequestTimeout, opt.RequestTimeoutRenewal = asInt(info["requestTimeout"]), asInt(info["requestTimeoutRenewal"])
		if ic, ok := info["interConnect"]; ok && ic != nil {
			raw, _ := json.Marshal(ic)
			if json.Unmarshal(raw, &opt.InterConnect) == nil {
				opt.HasInterConn = true
			}
		}
		opt.APIList = bodyValue(body, "apiList")
	}
	data, err := h.ctrl.Registry().Register(opt)
	if err != nil {
		h.logger.Error("Failed to register service for [" + opt.Name + "] " + err.Error())
	} else {
		resp.Result = true
		resp.Data = data
	}
	h.write(w, resp, true)
}

func bodyValue(body map[string]interface{}, key string) interface{} {
	if body == nil {
		return nil
	}
	return body[key]
}

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	}
	return ""
}

func asInt(v interface{}) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

func truthyValue(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	case float64:
		return t != 0
	}
	return true
}

// validVersion is soajs.core.libs version.validate: a value with digits in it.
func validVersion(v string) bool {
	for _, c := range v {
		if c >= '0' && c <= '9' {
			return true
		}
	}
	return false
}

// getRegistry answers the registry of ?env (the gateway's by default) for the service ?name (or
// ?serviceName) of ?type (service by default), with the gateway's host in this environment;
// ?setBy=loadByEnv loads another environment again.
func (h *maintenanceHandler) getRegistry(w http.ResponseWriter, route string, query map[string]string) {
	resp := h.response(route)
	env := query["env"]
	if env == "" {
		env = h.ctrl.Registry().GetEnvironment()
	}
	name := query["name"]
	if name == "" {
		name = query["serviceName"]
	}
	typ := query["type"]
	if typ == "" {
		typ = "service"
	}
	reg, err := h.ctrl.Registry().LoadByEnv(env, query["setBy"] == "loadByEnv")
	if err != nil {
		h.logger.Error(name + " " + err.Error())
	} else {
		resp.Result = true
		resp.Data = map[string]interface{}{}
	}
	if reg != nil {
		data := cloneAndFilterRegistry(reg, name, typ)
		if reg.ServiceConfig != nil && reg.ServiceConfig.Ports.Controller != 0 {
			var host interface{}
			gateway := h.ctrl.Gateway().Name
			if gw := reg.Services["controller"]; gw != nil && gw.Name != "" {
				gateway = gw.Name
			}
			if info, err := h.ctrl.Awareness().GetHost(gateway, ""); err == nil {
				host = info.Host
			}
			data["awareness"] = map[string]interface{}{"host": host, "port": reg.ServiceConfig.Ports.Controller}
		}
		resp.Data = data
	}
	h.write(w, resp, true)
}

// urlParamsConsumed are the URLParam options the Node.js mongo driver removes from a configuration
// when it connects with it (soajs.mongo constructMongoOptions).
var urlParamsConsumed = []string{"maxPoolSize", "wtimeoutMS", "slaveOk", "auto_reconnect"}

// dbDump is a database entry as services receive it. Until the registry is loaded again, the
// options and extraParam the Node.js gateway's connections consumed are not in it.
func dbDump(dump map[string]interface{}, reloaded bool) map[string]interface{} {
	out, _ := deepCopy(dump).(map[string]interface{})
	if out == nil {
		out = map[string]interface{}{}
	}
	if !reloaded {
		delete(out, "extraParam")
		if p, ok := out["URLParam"].(map[string]interface{}); ok {
			for _, k := range urlParamsConsumed {
				delete(p, k)
			}
		}
	}
	return out
}

func deepCopy(v interface{}) interface{} {
	raw, _ := json.Marshal(v)
	var out interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// cloneAndFilterRegistry is the registry a service receives (server/maintenance.js
// cloneAndFilterRegistry): the environment, its databases as the Node.js registry gives them (with
// coreDB.session), serviceConfig without cors, and without oauth except for the oauth service, the
// deployer without its kubernetes token, custom and resources; for a service, only the gateway
// entry and its own group and port; for a daemon, its own; otherwise every service and daemon.
func cloneAndFilterRegistry(reg *types.RegistryData, reqServiceName, reqServiceType string) map[string]interface{} {
	filtered := map[string]interface{}{}
	if reg == nil {
		return filtered
	}
	if reg.TimeLoaded != 0 {
		filtered["timeLoaded"] = reg.TimeLoaded
	}
	for k, v := range map[string]string{"name": reg.Name, "domain": reg.Domain, "sitePrefix": reg.SitePrefix, "apiPrefix": reg.APIPrefix, "protocol": reg.Protocol, "environment": reg.Environment} {
		if v != "" {
			filtered[k] = v
		}
	}
	if reg.Port != 0 {
		filtered["port"] = reg.Port
	}
	coreDB := map[string]interface{}{}
	for name, db := range reg.CoreDB {
		coreDB[name] = dbDump(db.Dump, reg.Reloaded)
	}
	if reg.SessionDB != nil {
		coreDB["session"] = reg.SessionDB
	}
	filtered["coreDB"] = coreDB
	meta := map[string]interface{}{}
	for name, db := range reg.TenantMetaDB {
		meta[name] = dbDump(db.Dump, reg.Reloaded)
	}
	filtered["tenantMetaDB"] = meta
	if reg.ServiceConfig != nil {
		serviceConfig, _ := deepCopy(reg.ServiceConfig).(map[string]interface{})
		delete(serviceConfig, "cors")
		if reqServiceName != "oauth" {
			delete(serviceConfig, "oauth")
		}
		filtered["serviceConfig"] = serviceConfig
	}
	if reg.Deployer != nil {
		deployer, _ := deepCopy(reg.Deployer).(map[string]interface{})
		if deployer["type"] == "container" {
			container, _ := deployer["container"].(map[string]interface{})
			k8s, _ := container["kubernetes"].(map[string]interface{})
			if conf, ok := k8s["configuration"].(map[string]interface{}); ok {
				delete(conf, "token")
			}
		}
		filtered["deployer"] = deployer
	}
	if reg.CustomRegistry != nil {
		filtered["custom"] = reg.CustomRegistry
	}
	if reg.Resources != nil {
		resources, _ := deepCopy(reg.Resources).(map[string]interface{})
		if clusters, ok := resources["cluster"].(map[string]interface{}); ok {
			for _, c := range clusters {
				cluster, _ := c.(map[string]interface{})
				if config, ok := cluster["config"].(map[string]interface{}); ok {
					cluster["config"] = dbDump(config, reg.Reloaded)
				}
			}
		}
		filtered["resources"] = resources
	}

	services := map[string]interface{}{}
	if gw, ok := reg.Services["controller"]; ok {
		services["controller"] = gw
	}
	switch reqServiceType {
	case "service":
		if svc, ok := reg.Services[reqServiceName]; ok && reqServiceName != "" {
			services[reqServiceName] = map[string]interface{}{"group": svc.Group, "port": svc.Port}
		}
	case "mdaemon":
		if d, ok := reg.Daemons[reqServiceName]; ok && reqServiceName != "" {
			filtered["daemons"] = map[string]interface{}{reqServiceName: map[string]interface{}{"group": d.Group, "port": d.Port}}
		}
	default:
		for name, svc := range reg.Services {
			services[name] = svc
		}
		filtered["daemons"] = reg.Daemons
	}
	filtered["services"] = services
	return filtered
}
