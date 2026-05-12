package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
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
	r.Get("/awarenessStat", handler.awarenessStat)

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
	h.respond(w, true, "heartbeat", nil)
}

func (h *maintenanceHandler) loadProvision(w http.ResponseWriter, r *http.Request) {
	if err := h.ctrl.ReloadProvision(); err != nil {
		h.respondError(w, "loadProvision", err)
		return
	}
	h.respond(w, true, "provision reloaded", nil)
}

func (h *maintenanceHandler) reloadRegistry(w http.ResponseWriter, r *http.Request) {
	if err := h.ctrl.ReloadRegistry(); err != nil {
		h.respondError(w, "reloadRegistry", err)
		return
	}
	h.respond(w, true, "registry reloaded", nil)
}

func (h *maintenanceHandler) awarenessStat(w http.ResponseWriter, r *http.Request) {
	stats := h.ctrl.Awareness().GetStats()
	h.respond(w, true, "awareness stats", stats)
}

func (h *maintenanceHandler) respond(w http.ResponseWriter, result bool, msg string, data interface{}) {
	resp := MaintenanceResponse{
		Result: result,
		TS:     time.Now().UnixMilli(),
		Service: ServiceInfo{
			Name:    h.ctrl.cfg.ServiceName,
			Version: h.ctrl.cfg.ServiceVersion,
			Type:    "service",
		},
		Data: data,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func (h *maintenanceHandler) respondError(w http.ResponseWriter, op string, err error) {
	h.logger.Error("maintenance operation failed",
		zap.String("operation", op),
		zap.Error(err),
	)

	resp := MaintenanceResponse{
		Result: false,
		TS:     time.Now().UnixMilli(),
		Service: ServiceInfo{
			Name:    h.ctrl.cfg.ServiceName,
			Version: h.ctrl.cfg.ServiceVersion,
			Type:    "service",
		},
		Error: err.Error(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(resp)
}
