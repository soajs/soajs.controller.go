package middleware

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.uber.org/zap"
)

// MonitorConfig holds monitoring configuration from custom registry
type MonitorConfig struct {
	ReqInfo     bool                   `json:"req_info"`
	ReqHeader   bool                   `json:"req_header"`
	ReqQuery    bool                   `json:"req_query"`
	ReqBody     bool                   `json:"req_body"`
	ReqResponse bool                   `json:"req_response"`
	Blacklist   []string               `json:"blacklist"`
	Whitelist   []string               `json:"whitelist"`
	Buffer      *MonitorBufferConfig   `json:"buffer"`
}

// MonitorBufferConfig holds buffer configuration
type MonitorBufferConfig struct {
	Time  int `json:"time"`  // milliseconds
	Limit int `json:"limit"` // count threshold
}

// MonitorDocument represents a monitoring record
type MonitorDocument struct {
	Name     string                 `json:"name"`
	Version  string                 `json:"version"`
	API      string                 `json:"api"`
	Method   string                 `json:"method"`
	Port     int                    `json:"port,omitempty"`
	URL      string                 `json:"url,omitempty"`
	Host     string                 `json:"host,omitempty"`
	Headers  map[string][]string    `json:"headers,omitempty"`
	Query    map[string][]string    `json:"query,omitempty"`
	Body     string                 `json:"body,omitempty"`
	Response string                 `json:"response,omitempty"`
	Time     MonitorTime            `json:"time"`
}

// MonitorTime holds timing information
type MonitorTime struct {
	ReqStart     int64 `json:"req_start,omitempty"`
	ReqBodyStart int64 `json:"req_body_start,omitempty"`
	ReqBodyEnd   int64 `json:"req_body_end,omitempty"`
	ResStart     int64 `json:"res_start,omitempty"`
	ResEnd       int64 `json:"res_end,omitempty"`
}

// MonitorBuffer manages buffered monitor documents
type MonitorBuffer struct {
	mu              sync.Mutex
	buffer          []MonitorDocument
	flushTimer      *time.Timer
	flushInProgress bool
	config          *MonitorBufferConfig
	reg             registry.Registry
	aw              awareness.Awareness
	logger          *zap.Logger
}

var globalMonitorBuffer *MonitorBuffer
var monitorBufferOnce sync.Once

// GetMonitorBuffer returns the singleton monitor buffer
func GetMonitorBuffer(reg registry.Registry, aw awareness.Awareness, logger *zap.Logger) *MonitorBuffer {
	monitorBufferOnce.Do(func() {
		globalMonitorBuffer = &MonitorBuffer{
			buffer: make([]MonitorDocument, 0),
			reg:    reg,
			aw:     aw,
			logger: logger,
		}
	})
	return globalMonitorBuffer
}

// SetConfig updates the buffer configuration
func (mb *MonitorBuffer) SetConfig(config *MonitorBufferConfig) {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.config = config
}

// Add adds a document to the buffer
func (mb *MonitorBuffer) Add(doc MonitorDocument) {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	mb.buffer = append(mb.buffer, doc)

	if mb.config == nil {
		// No buffering configured, flush immediately
		mb.flushLocked()
		return
	}

	// Check count threshold
	if mb.config.Limit > 0 && len(mb.buffer) >= mb.config.Limit {
		mb.flushLocked()
		return
	}

	// Start timer if not running and time threshold is set
	if mb.config.Time > 0 && mb.flushTimer == nil {
		mb.flushTimer = time.AfterFunc(time.Duration(mb.config.Time)*time.Millisecond, func() {
			mb.Flush()
		})
	}
}

// Flush sends all buffered documents to soamonitor
func (mb *MonitorBuffer) Flush() {
	mb.mu.Lock()
	defer mb.mu.Unlock()
	mb.flushLocked()
}

// flushLocked sends buffered documents (must be called with lock held)
func (mb *MonitorBuffer) flushLocked() {
	if mb.flushInProgress || len(mb.buffer) == 0 {
		return
	}

	mb.flushInProgress = true

	// Clear timer
	if mb.flushTimer != nil {
		mb.flushTimer.Stop()
		mb.flushTimer = nil
	}

	// Take current buffer
	items := mb.buffer
	mb.buffer = make([]MonitorDocument, 0)

	// Send async
	go mb.sendItems(items)
}

// sendItems sends items to soamonitor service
func (mb *MonitorBuffer) sendItems(items []MonitorDocument) {
	defer func() {
		mb.mu.Lock()
		mb.flushInProgress = false
		mb.mu.Unlock()
	}()

	// Get soamonitor host
	hostInfo, err := mb.aw.GetHost("soamonitor", "1")
	if err != nil || hostInfo == nil {
		mb.logger.Debug("Monitor buffer flush: Unable to find host for soamonitor")
		// Put items back in buffer
		mb.mu.Lock()
		mb.buffer = append(items, mb.buffer...)
		mb.mu.Unlock()
		return
	}

	// Get port from registry
	port := 4050
	regData := mb.reg.Get()
	if regData != nil && regData.Services != nil {
		if svc, ok := regData.Services["soamonitor"]; ok && svc.Port > 0 {
			port = svc.Port
		}
	}

	// Build request
	var api string
	var body interface{}
	if len(items) == 1 {
		api = "/monitor/item"
		body = items[0]
	} else {
		api = "/monitor/items"
		body = map[string]interface{}{"items": items}
	}

	uri := fmt.Sprintf("http://%s:%d%s", hostInfo.Host, port, api)
	jsonBody, err := json.Marshal(body)
	if err != nil {
		mb.logger.Debug("Monitor buffer: failed to marshal", zap.Error(err))
		return
	}

	req, err := http.NewRequest("POST", uri, bytes.NewReader(jsonBody))
	if err != nil {
		mb.logger.Debug("Monitor buffer: failed to create request", zap.Error(err))
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		mb.logger.Debug("Monitor buffer flush failed", zap.Error(err))
		return
	}
	defer resp.Body.Close()

	mb.logger.Debug("Monitor buffer flushed", zap.Int("items", len(items)))
}

// Monitor middleware tracks request/response metrics
func Monitor(reg registry.Registry, aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			// Get monitor config
			regData := reg.Get()
			monitorConfig := getMonitorConfig(regData)

			if monitorConfig == nil || soajsCtx == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Check blacklist/whitelist
			serviceName := ""
			if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
				serviceName = soajsCtx.Controller.ServiceParams.ServiceName
				if serviceName == "" && soajsCtx.Controller.ServiceParams.URLInfo != nil {
					serviceName = soajsCtx.Controller.ServiceParams.URLInfo.ServiceName
				}
			}

			if isServiceBlacklisted(serviceName, monitorConfig) {
				next.ServeHTTP(w, r)
				return
			}

			// Initialize monitor document
			doc := MonitorDocument{
				Name:   serviceName,
				Method: r.Method,
				Time: MonitorTime{
					ReqStart: time.Now().UnixMilli(),
				},
			}

			if soajsCtx.Controller != nil && soajsCtx.Controller.ServiceParams != nil {
				if soajsCtx.Controller.ServiceParams.URLInfo != nil {
					doc.API = soajsCtx.Controller.ServiceParams.URLInfo.ServicePath
					doc.Version = soajsCtx.Controller.ServiceParams.URLInfo.ServiceVersion
				}
			}

			if monitorConfig.ReqInfo {
				doc.Host = r.Host
				doc.URL = r.URL.String()
			}

			if monitorConfig.ReqHeader {
				doc.Headers = r.Header
			}

			if monitorConfig.ReqQuery {
				doc.Query = r.URL.Query()
			}

			// Wrap response writer to capture response
			rw := &monitorResponseWriter{
				ResponseWriter: w,
				statusCode:     200,
			}

			next.ServeHTTP(rw, r)

			// Record response timing
			doc.Time.ResEnd = time.Now().UnixMilli()

			if monitorConfig.ReqResponse && len(rw.body) > 0 {
				doc.Response = string(rw.body)
			}

			// Add to buffer
			buffer := GetMonitorBuffer(reg, aw, soajsCtx.Log)
			buffer.SetConfig(monitorConfig.Buffer)
			buffer.Add(doc)
		})
	}
}

// monitorResponseWriter wraps http.ResponseWriter to capture response
type monitorResponseWriter struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (rw *monitorResponseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *monitorResponseWriter) Write(b []byte) (int, error) {
	// Limit captured body size to 10MB
	maxSize := 10 * 1024 * 1024
	if len(rw.body)+len(b) <= maxSize {
		rw.body = append(rw.body, b...)
	}
	return rw.ResponseWriter.Write(b)
}

// Flush implements http.Flusher - required for streaming/chunked responses
func (rw *monitorResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker - required for WebSocket upgrades
func (rw *monitorResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("hijack not supported")
}

// toSlice converts interface{} to []interface{}, handling bson.A
func toSlice(v interface{}) []interface{} {
	switch s := v.(type) {
	case []interface{}:
		return s
	case bson.A:
		return s
	default:
		return nil
	}
}

// getMonitorConfig extracts monitor config from registry
func getMonitorConfig(regData *types.RegistryData) *MonitorConfig {
	if regData == nil || regData.CustomRegistry == nil {
		return nil
	}

	gateway, ok := regData.CustomRegistry["gateway"]
	if !ok {
		return nil
	}

	// Handle both map[string]interface{} and bson.M
	var gatewayValue map[string]interface{}
	switch v := gateway.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			gatewayValue = toMap(val)
		} else {
			gatewayValue = v
		}
	case bson.M:
		if val, exists := v["value"]; exists {
			gatewayValue = toMap(val)
		} else {
			gatewayValue = v
		}
	default:
		return nil
	}

	if gatewayValue == nil {
		return nil
	}

	gotoService := toMap(gatewayValue["gotoService"])
	if gotoService == nil {
		return nil
	}

	monitorData := toMap(gotoService["monitor"])
	if monitorData == nil {
		return nil
	}

	config := &MonitorConfig{}

	if v, ok := monitorData["req_info"].(bool); ok {
		config.ReqInfo = v
	}
	if v, ok := monitorData["req_header"].(bool); ok {
		config.ReqHeader = v
	}
	if v, ok := monitorData["req_query"].(bool); ok {
		config.ReqQuery = v
	}
	if v, ok := monitorData["req_body"].(bool); ok {
		config.ReqBody = v
	}
	if v, ok := monitorData["req_response"].(bool); ok {
		config.ReqResponse = v
	}

	if bl := toSlice(monitorData["blacklist"]); bl != nil {
		for _, s := range bl {
			if str, ok := s.(string); ok {
				config.Blacklist = append(config.Blacklist, str)
			}
		}
	}

	if wl := toSlice(monitorData["whitelist"]); wl != nil {
		for _, s := range wl {
			if str, ok := s.(string); ok {
				config.Whitelist = append(config.Whitelist, str)
			}
		}
	}

	if buffer := toMap(monitorData["buffer"]); buffer != nil {
		config.Buffer = &MonitorBufferConfig{}
		if t, ok := buffer["time"].(float64); ok {
			config.Buffer.Time = int(t)
		}
		if l, ok := buffer["limit"].(float64); ok {
			config.Buffer.Limit = int(l)
		}
	}

	return config
}

// GotoServiceConfig holds gotoService configuration from custom registry
type GotoServiceConfig struct {
	RenewReqMonitorOff bool           `json:"renewReqMonitorOff"`
	Monitor            *MonitorConfig `json:"monitor"`
}

// GetGotoServiceConfig extracts gotoService config from custom registry
// Path: registry.custom.gateway.value.gotoService
func GetGotoServiceConfig(regData *types.RegistryData) *GotoServiceConfig {
	if regData == nil || regData.CustomRegistry == nil {
		return nil
	}

	gateway, ok := regData.CustomRegistry["gateway"]
	if !ok {
		return nil
	}

	var gatewayValue interface{}
	switch v := gateway.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			gatewayValue = val
		} else {
			gatewayValue = v
		}
	default:
		return nil
	}

	gatewayMap, ok := gatewayValue.(map[string]interface{})
	if !ok {
		return nil
	}

	gotoService, ok := gatewayMap["gotoService"].(map[string]interface{})
	if !ok {
		return nil
	}

	config := &GotoServiceConfig{}

	if off, ok := gotoService["renewReqMonitorOff"].(bool); ok {
		config.RenewReqMonitorOff = off
	}

	return config
}

// isServiceBlacklisted checks if service should be excluded from monitoring
func isServiceBlacklisted(serviceName string, config *MonitorConfig) bool {
	// Check whitelist first
	if len(config.Whitelist) > 0 {
		for _, s := range config.Whitelist {
			if s == serviceName {
				return false
			}
		}
		return true
	}

	// Check blacklist
	if len(config.Blacklist) > 0 {
		for _, s := range config.Blacklist {
			if s == serviceName {
				return true
			}
		}
	}

	return false
}

// isTokenRenewRequest checks if the request is an OAuth token renew/refresh request
// These requests are typically high-frequency and may be excluded from monitoring
// when renewReqMonitorOff is enabled
func isTokenRenewRequest(serviceName string, urlInfo *types.URLInfo) bool {
	if urlInfo == nil {
		return false
	}

	// Check if this is an OAuth-related service
	if serviceName != "oauth" && serviceName != "authenticator" {
		return false
	}

	// Check for token-related paths that indicate a renew/refresh request
	path := urlInfo.ServicePath
	renewPaths := []string{
		"/token",
		"/refresh/token",
		"/access/token",
		"/renew",
	}

	for _, renewPath := range renewPaths {
		if path == renewPath || strings.HasPrefix(path, renewPath+"/") || strings.HasPrefix(path, renewPath+"?") {
			return true
		}
	}

	return false
}
