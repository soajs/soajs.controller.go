package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/version"
	"go.uber.org/zap"
)

// MaintenanceResponse represents a maintenance endpoint response
type MaintenanceResponse struct {
	Result  bool        `json:"result"`
	TS      int64       `json:"ts"`
	Service ServiceInfo `json:"service"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}

// ServiceInfo holds service metadata
type ServiceInfo struct {
	Name    string `json:"service"`
	Version string `json:"version"`
	Type    string `json:"type"`
	Route   string `json:"route,omitempty"`
}

// NewMaintenanceServer creates a new maintenance HTTP server
func NewMaintenanceServer(port int, ctrl *Controller, logger *zap.Logger) *http.Server {
	r := chi.NewRouter()

	handler := &maintenanceHandler{
		ctrl:   ctrl,
		logger: logger,
	}

	// Maintenance routes
	r.Get("/favicon.ico", handler.favicon)
	r.Get("/heartbeat", handler.heartbeat)
	r.Get("/loadProvision", handler.loadProvision)
	r.Get("/reloadRegistry", handler.reloadRegistry)
	r.Get("/getRegistry", handler.getRegistry)
	r.Get("/awarenessStat", handler.awarenessStat)
	r.Get("/register", handler.register)
	r.Post("/register", handler.register)
	r.Get("/proxySocket", handler.proxySocket)

	return &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}
}

type maintenanceHandler struct {
	ctrl   *Controller
	logger *zap.Logger
}

func (h *maintenanceHandler) favicon(w http.ResponseWriter, r *http.Request) {
	// Return empty response for favicon
	w.WriteHeader(http.StatusNoContent)
}

func (h *maintenanceHandler) heartbeat(w http.ResponseWriter, r *http.Request) {
	h.respondWithRoute(w, true, nil, "/heartbeat")
}

func (h *maintenanceHandler) loadProvision(w http.ResponseWriter, r *http.Request) {
	if err := h.ctrl.ReloadProvision(); err != nil {
		h.respondError(w, "/loadProvision", err)
		return
	}
	h.respondWithRoute(w, true, nil, "/loadProvision")
}

func (h *maintenanceHandler) reloadRegistry(w http.ResponseWriter, r *http.Request) {
	if err := h.ctrl.ReloadRegistry(); err != nil {
		h.respondError(w, "/reloadRegistry", err)
		return
	}
	h.respondWithRoute(w, true, cloneAndFilterRegistry(h.ctrl.Registry().Get(), "", ""), "/reloadRegistry")
}

func (h *maintenanceHandler) getRegistry(w http.ResponseWriter, r *http.Request) {
	reqServiceName := r.URL.Query().Get("serviceName")
	reqServiceType := r.URL.Query().Get("type")
	if reqServiceType == "" {
		reqServiceType = "service"
	}

	regData := h.ctrl.Registry().Get()
	if regData == nil {
		h.respondError(w, "/getRegistry", fmt.Errorf("registry not loaded"))
		return
	}

	filteredRegistry := cloneAndFilterRegistry(regData, reqServiceName, reqServiceType)

	// Add awareness info
	controllerPort := 4000
	if regData.ServiceConfig != nil && regData.ServiceConfig.Ports.Controller > 0 {
		controllerPort = regData.ServiceConfig.Ports.Controller
	}
	filteredRegistry["awareness"] = map[string]interface{}{
		"host": "127.0.0.1",
		"port": controllerPort,
	}

	h.respondWithRoute(w, true, filteredRegistry, "/getRegistry")
}

// cloneAndFilterRegistry is the registry a service receives (Node.js server/maintenance.js:51-139):
// serviceConfig without cors, and without oauth except for the oauth service; the deployer without
// its kubernetes token; for a service, only the gateway entry and its own group and port.
func cloneAndFilterRegistry(reg *types.RegistryData, reqServiceName, reqServiceType string) map[string]interface{} {
	filtered := map[string]interface{}{}
	if reg == nil {
		return filtered
	}
	filtered["timeLoaded"] = reg.TimeLoaded
	if reg.Name != "" {
		filtered["name"] = reg.Name
	}
	if reg.Domain != "" {
		filtered["domain"] = reg.Domain
	}
	if reg.SitePrefix != "" {
		filtered["sitePrefix"] = reg.SitePrefix
	}
	if reg.APIPrefix != "" {
		filtered["apiPrefix"] = reg.APIPrefix
	}
	if reg.Protocol != "" {
		filtered["protocol"] = reg.Protocol
	}
	if reg.Port != 0 {
		filtered["port"] = reg.Port
	}
	if reg.Environment != "" {
		filtered["environment"] = reg.Environment
	}
	filtered["coreDB"] = reg.CoreDB
	filtered["tenantMetaDB"] = reg.TenantMetaDB
	if reg.ServiceConfig != nil {
		var serviceConfig map[string]interface{}
		clone(reg.ServiceConfig, &serviceConfig)
		delete(serviceConfig, "cors")
		if reqServiceName != "oauth" {
			delete(serviceConfig, "oauth")
		}
		filtered["serviceConfig"] = serviceConfig
	}
	if reg.Deployer != nil {
		var deployer map[string]interface{}
		clone(reg.Deployer, &deployer)
		if deployer["type"] == "container" {
			container, _ := deployer["container"].(map[string]interface{})
			k8s, _ := container["kubernetes"].(map[string]interface{})
			if conf, ok := k8s["configuration"].(map[string]interface{}); ok {
				delete(conf, "token")
			}
		}
		filtered["deployer"] = deployer
	}
	filtered["custom"] = reg.CustomRegistry
	filtered["resources"] = reg.Resources

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

// clone deep-copies through JSON, so filtering never touches the registry in use.
func clone(in, out interface{}) {
	raw, _ := json.Marshal(in)
	_ = json.Unmarshal(raw, out)
}

func (h *maintenanceHandler) awarenessStat(w http.ResponseWriter, r *http.Request) {
	regData := h.ctrl.Registry().Get()
	data := map[string]interface{}{}
	if regData != nil {
		data["services"] = regData.Services
		data["daemons"] = regData.Daemons
	}
	h.respondWithRoute(w, true, data, "/awarenessStat")
}

// RegisterRequest holds the service registration request data
type RegisterRequest struct {
	Name                  string `json:"name"`
	Group                 string `json:"group"`
	Port                  int    `json:"port"`
	IP                    string `json:"ip"`
	Type                  string `json:"type"`
	Version               string `json:"version"`
	ExtKeyRequired        bool   `json:"extKeyRequired"`
	OAuth                 bool   `json:"oauth"`
	RequestTimeout        int    `json:"requestTimeout"`
	RequestTimeoutRenewal int    `json:"requestTimeoutRenewal"`
}

func (h *maintenanceHandler) register(w http.ResponseWriter, r *http.Request) {
	// Get registration info from query params or POST body
	var regInfo RegisterRequest

	if r.Method == "POST" {
		// Parse POST body
		if err := json.NewDecoder(r.Body).Decode(&regInfo); err != nil {
			// Try query params as fallback
			regInfo = h.parseRegisterQuery(r)
		}
	} else {
		// GET request - parse query params
		regInfo = h.parseRegisterQuery(r)
	}

	// Validate required fields
	if regInfo.Name == "" || regInfo.IP == "" || regInfo.Version == "" {
		h.logger.Warn("invalid registration request",
			zap.String("name", regInfo.Name),
			zap.String("ip", regInfo.IP),
			zap.String("version", regInfo.Version),
		)
		h.respondWithRoute(w, false, nil, "/register")
		return
	}

	// Default port if not provided
	if regInfo.Port == 0 {
		// Try to get from registry
		regData := h.ctrl.Registry().Get()
		if regData != nil && regData.Services != nil {
			if svc, ok := regData.Services[regInfo.Name]; ok {
				regInfo.Port = svc.Port
			}
		}
	}

	// Register the host
	hostname, _ := os.Hostname()
	hostInfo := types.HostRecord{
		Env:      h.ctrl.Registry().GetEnvironment(),
		Name:     regInfo.Name,
		Version:  regInfo.Version,
		IP:       regInfo.IP,
		Port:     regInfo.Port,
		Hostname: strings.ToLower(hostname),
	}

	if err := h.ctrl.Registry().RegisterHost(hostInfo); err != nil {
		h.logger.Error("failed to register service host",
			zap.String("name", regInfo.Name),
			zap.String("ip", regInfo.IP),
			zap.Error(err),
		)
		h.respondWithRoute(w, false, nil, "/register")
		return
	}

	h.logger.Info("service registered",
		zap.String("name", regInfo.Name),
		zap.String("ip", regInfo.IP),
		zap.Int("port", regInfo.Port),
		zap.String("version", regInfo.Version),
	)

	// Return service info
	regData := h.ctrl.Registry().Get()
	var responseData interface{}
	if regData != nil && regData.Services != nil {
		if svc, ok := regData.Services[regInfo.Name]; ok {
			responseData = svc
		}
	}

	h.respondWithRoute(w, true, responseData, "/register")
}

func (h *maintenanceHandler) parseRegisterQuery(r *http.Request) RegisterRequest {
	port := 0
	if p := r.URL.Query().Get("port"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}

	return RegisterRequest{
		Name:    r.URL.Query().Get("name"),
		Group:   r.URL.Query().Get("group"),
		Port:    port,
		IP:      r.URL.Query().Get("ip"),
		Type:    r.URL.Query().Get("type"),
		Version: r.URL.Query().Get("version"),
	}
}

func (h *maintenanceHandler) respondWithRoute(w http.ResponseWriter, result bool, data interface{}, route string) {
	resp := MaintenanceResponse{
		Result: result,
		TS:     time.Now().UnixMilli(),
		Service: ServiceInfo{
			Name:    "soajs.controller",
			Version: version.Get(),
			Type:    "rest",
			Route:   route,
		},
		Data: data,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *maintenanceHandler) respondError(w http.ResponseWriter, route string, err error) {
	h.logger.Error("maintenance operation failed",
		zap.String("route", route),
		zap.Error(err),
	)

	resp := MaintenanceResponse{
		Result: false,
		TS:     time.Now().UnixMilli(),
		Service: ServiceInfo{
			Name:    "soajs.controller",
			Version: version.Get(),
			Type:    "rest",
			Route:   route,
		},
		Error: err.Error(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(resp)
}

func (h *maintenanceHandler) proxySocket(w http.ResponseWriter, r *http.Request) {
	// ProxySocket returns information about socket proxy configuration
	// This is used for WebSocket routing
	regData := h.ctrl.Registry().Get()

	var socketInfo interface{}
	if regData != nil && regData.ServiceConfig != nil {
		// Return socket configuration if available
		socketInfo = map[string]interface{}{
			"host": "127.0.0.1",
			"port": regData.ServiceConfig.Ports.Controller,
		}
	}

	h.respondWithRoute(w, true, socketInfo, "/proxySocket")
}
