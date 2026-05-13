package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/soajs/soajs.controller.go/pkg/types"
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
	r.Get("/heartbeat", handler.heartbeat)
	r.Get("/loadProvision", handler.loadProvision)
	r.Get("/reloadRegistry", handler.reloadRegistry)
	r.Get("/getRegistry", handler.getRegistry)
	r.Get("/awarenessStat", handler.awarenessStat)
	r.Get("/register", handler.register)
	r.Post("/register", handler.register)

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
	// Return the registry data like Node.js does
	regData := h.ctrl.Registry().Get()
	h.respondWithRoute(w, true, regData, "/reloadRegistry")
}

func (h *maintenanceHandler) getRegistry(w http.ResponseWriter, r *http.Request) {
	// Get query parameters
	reqServiceName := r.URL.Query().Get("name")
	if reqServiceName == "" {
		reqServiceName = r.URL.Query().Get("serviceName")
	}
	reqServiceType := r.URL.Query().Get("type")
	if reqServiceType == "" {
		reqServiceType = "service"
	}

	regData := h.ctrl.Registry().Get()
	if regData == nil {
		h.respondError(w, "/getRegistry", fmt.Errorf("registry not loaded"))
		return
	}

	// Build filtered registry like Node.js does
	filteredRegistry := h.cloneAndFilterRegistry(regData, reqServiceName, reqServiceType)

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

func (h *maintenanceHandler) cloneAndFilterRegistry(reg *types.RegistryData, reqServiceName, reqServiceType string) map[string]interface{} {
	filtered := make(map[string]interface{})

	if reg == nil {
		return filtered
	}

	// Copy basic fields
	filtered["timeLoaded"] = reg.TimeLoaded
	filtered["name"] = reg.Name
	filtered["environment"] = reg.Environment
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
	if reg.Port > 0 {
		filtered["port"] = reg.Port
	}

	// Copy coreDB and tenantMetaDB
	if reg.CoreDB != nil {
		filtered["coreDB"] = reg.CoreDB
	}
	if reg.TenantMetaDB != nil {
		filtered["tenantMetaDB"] = reg.TenantMetaDB
	}

	// Copy serviceConfig (filter out cors, and oauth unless requesting service is oauth)
	if reg.ServiceConfig != nil {
		serviceConfig := map[string]interface{}{
			"logger":    reg.ServiceConfig.Logger,
			"ports":     reg.ServiceConfig.Ports,
			"awareness": reg.ServiceConfig.Awareness,
			"key":       reg.ServiceConfig.Key,
		}
		if reg.ServiceConfig.Cookie.Secret != "" {
			serviceConfig["cookie"] = reg.ServiceConfig.Cookie
		}
		if reg.ServiceConfig.Session.Name != "" {
			serviceConfig["session"] = reg.ServiceConfig.Session
		}
		// Only include oauth config if requesting service is oauth
		if reqServiceName == "oauth" && len(reg.ServiceConfig.OAuth.Grants) > 0 {
			serviceConfig["oauth"] = reg.ServiceConfig.OAuth
		}
		if reg.ServiceConfig.Throttling != nil {
			serviceConfig["throttling"] = reg.ServiceConfig.Throttling
		}
		filtered["serviceConfig"] = serviceConfig
	}

	// Copy deployer
	if reg.Deployer != nil {
		filtered["deployer"] = reg.Deployer
	}

	// Copy custom and resources
	if reg.CustomRegistry != nil {
		filtered["custom"] = reg.CustomRegistry
	}
	if reg.Resources != nil {
		filtered["resources"] = reg.Resources
	}

	// Filter services based on request
	services := make(map[string]interface{})
	if reg.Services != nil {
		// Always include controller
		if ctrl, ok := reg.Services["controller"]; ok {
			services["controller"] = ctrl
		}

		if reqServiceType == "service" && reqServiceName != "" {
			// Only include the requested service (minimal info)
			if svc, ok := reg.Services[reqServiceName]; ok {
				services[reqServiceName] = map[string]interface{}{
					"group": svc.Group,
					"port":  svc.Port,
				}
			}
		} else if reqServiceType != "mdaemon" {
			// Include all services
			for name, svc := range reg.Services {
				services[name] = svc
			}
		}
	}
	filtered["services"] = services

	// Handle daemons
	if reqServiceType == "mdaemon" && reqServiceName != "" {
		daemons := make(map[string]interface{})
		if reg.Daemons != nil {
			if daemon, ok := reg.Daemons[reqServiceName]; ok {
				daemons[reqServiceName] = map[string]interface{}{
					"group": daemon.Group,
					"port":  daemon.Port,
				}
			}
		}
		filtered["daemons"] = daemons
	} else if reg.Daemons != nil {
		filtered["daemons"] = reg.Daemons
	}

	return filtered
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
	hostInfo := types.HostInfo{
		Name:    regInfo.Name,
		Version: regInfo.Version,
		Host:    regInfo.IP,
		Port:    regInfo.Port,
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
			Version: "4.3.4",
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
			Version: "4.3.4",
			Type:    "rest",
			Route:   route,
		},
		Error: err.Error(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(resp)
}
