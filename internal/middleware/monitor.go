package middleware

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// defaultMonitorSize is SOAJS_MAX_BODY_SIZE and SOAJS_MAX_RESPONSE_SIZE without a value: 10 MB.
const defaultMonitorSize = 10 * 1024 * 1024

var monitorTextRe = regexp.MustCompile(`(?i)application/json|text/plain`)

// monitorRecord is the soamonitor document of a redirected request (redirectToService.js): the
// service, version, path and method, then with req_info its port, url and host, with req_header
// the request's headers, with req_query its query, with req_body the body of a JSON or text
// POST, PUT, PATCH or DELETE, and with req_response the answer. A document is sent only with
// req_response, once the answer ended or failed.
type monitorRecord struct {
	mu       sync.Mutex
	config   map[string]interface{}
	times    object
	doc      object
	body     *bytes.Buffer
	bodyNote string
	bodyOver bool
	bodyOK   bool
	resp     *bytes.Buffer
	respNote string
	respOver bool
	respOK   bool
	maxBody  int
	maxResp  int
}

// newMonitorRecord starts the record of a request, nil when gotoService.monitor is not set or
// leaves the service out (blacklist, or a whitelist without it).
func newMonitorRecord(reg *types.RegistryData, r *http.Request, params *types.ServiceParams, host string) *monitorRecord {
	gateway, _ := reg.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	gotoService, _ := value["gotoService"].(map[string]interface{})
	config, ok := gotoService["monitor"].(map[string]interface{})
	if !ok || !truthy(gotoService["monitor"]) {
		return nil
	}
	name := params.URLInfo.ServiceName
	if list, ok := config["blacklist"].([]interface{}); ok && len(list) > 0 && containsValue(list, name) {
		return nil
	}
	if list, ok := config["whitelist"].([]interface{}); ok && len(list) > 0 && !containsValue(list, name) {
		return nil
	}
	m := &monitorRecord{config: config, maxBody: envSize("SOAJS_MAX_BODY_SIZE"), maxResp: envSize("SOAJS_MAX_RESPONSE_SIZE")}
	m.times = object{{"req_start", time.Now().UnixMilli()}}
	m.doc = object{{"name", name}, {"version", params.URLInfo.ServiceVersion}, {"api", params.URLInfo.FullPath}, {"method", r.Method}}
	if truthy(config["req_info"]) {
		port := 0
		if svc := reg.Services[name]; svc != nil {
			port = svc.Port
		}
		m.doc = append(m.doc, field{"port", port}, field{"url", params.URLInfo.URL}, field{"host", host})
	}
	if truthy(config["req_header"]) {
		m.doc = append(m.doc, field{"headers", nodeHeaders(r)})
	}
	if truthy(config["req_query"]) {
		query := map[string]interface{}{}
		for k, v := range r.URL.Query() {
			query[k] = v[len(v)-1]
		}
		if _, ok := query["access_token"]; !ok && params.AccessToken != "" {
			query["access_token"] = params.AccessToken
		}
		m.doc = append(m.doc, field{"query", query})
	}
	return m
}

func envSize(name string) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return defaultMonitorSize
}

func containsValue(list []interface{}, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

// nodeHeaders are the request's headers as Node.js holds them: lowercase names, repeated values
// joined with ", " (a cookie with "; ").
func nodeHeaders(r *http.Request) map[string]interface{} {
	out := map[string]interface{}{}
	names := make([]string, 0, len(r.Header))
	for k := range r.Header {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		sep := ", "
		lk := strings.ToLower(k)
		if lk == "cookie" {
			sep = "; "
		}
		out[lk] = strings.Join(r.Header[k], sep)
	}
	if r.Host != "" {
		out["host"] = r.Host
	}
	return out
}

// captureBody records a chunk of the request body, for req_body on a JSON or text POST, PUT,
// PATCH or DELETE; another content type is noted as not allowed.
func (m *monitorRecord) captureBody(r *http.Request, chunk []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ct := r.Header.Get("Content-Type")
	if streamRe.MatchString(ct) || !monitorTextRe.MatchString(ct) {
		m.bodyNote = `{"contentType": "is stream or not allowed", "value': "` + ct + `"}`
		m.body = nil
		return
	}
	m.bodyOK = true
	if m.body == nil && !m.bodyOver {
		m.times.set("req_body_start", time.Now().UnixMilli())
		m.body = &bytes.Buffer{}
	}
	if m.bodyOver {
		return
	}
	if m.body.Len()+len(chunk) > m.maxBody {
		m.bodyOver = true
		m.body = nil
		m.bodyNote = `{"error": "Request body exceeds maximum size limit"}`
		return
	}
	m.body.Write(chunk)
}

// bodyEnd marks the end of a captured body.
func (m *monitorRecord) bodyEnd() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.bodyOK {
		m.times.set("req_body_end", time.Now().UnixMilli())
	}
}

// responseStart and captureResponse record the answer, for req_response.
func (m *monitorRecord) responseStart(contentType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.respOK = monitorTextRe.MatchString(contentType) && !streamRe.MatchString(contentType)
	if !m.respOK {
		m.respNote = `{"contentType": "is stream or not allowed", "value': "` + contentType + `"}`
	}
	m.times.set("res_start", time.Now().UnixMilli())
}

func (m *monitorRecord) captureResponse(chunk []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.respOK || m.respOver {
		return
	}
	if m.resp == nil {
		m.resp = &bytes.Buffer{}
	}
	if m.resp.Len()+len(chunk) > m.maxResp {
		m.respOver = true
		m.resp = nil
		m.respNote = `{"error": "Response body exceeds maximum size limit"}`
		return
	}
	m.resp.Write(chunk)
}

func (m *monitorRecord) wantsBody(method string) bool {
	if m == nil || !truthy(m.config["req_body"]) {
		return false
	}
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func (m *monitorRecord) wantsResponse() bool {
	return m != nil && truthy(m.config["req_response"])
}

// document is the record as sent to soamonitor.
func (m *monitorRecord) document() object {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.times.set("res_end", time.Now().UnixMilli())
	doc := append(object{{"time", m.times}}, m.doc...)
	if m.body != nil && m.body.Len() > 0 {
		doc = append(doc, field{"body", m.body.String()})
	} else if m.bodyNote != "" {
		doc = append(doc, field{"body", m.bodyNote})
	}
	if m.resp != nil && m.resp.Len() > 0 {
		doc = append(doc, field{"response", m.resp.String()})
	} else if m.respNote != "" {
		doc = append(doc, field{"response", m.respNote})
	}
	return doc
}

// Monitor sends the records to soamonitor@1 (soamonitor port, else 4050): one by one to
// /monitor/item with the request's soajsinjectobj, or, with monitor.buffer time or limit, in
// batches to /monitor/items, flushed at the limit or after the time. A batch with no soamonitor
// host is kept for the next flush; a batch that fails is dropped.
type Monitor struct {
	mu         sync.Mutex
	buffer     []interface{}
	timer      *time.Timer
	inProgress bool
	aw         awareness.Awareness
	reg        func() *types.RegistryData
	logger     *zap.Logger
	client     *http.Client
}

// NewMonitor returns the sender of monitor records.
func NewMonitor(reg func() *types.RegistryData, aw awareness.Awareness, logger *zap.Logger) *Monitor {
	return &Monitor{aw: aw, reg: reg, logger: logger, client: &http.Client{Timeout: 8 * time.Second}}
}

func (mo *Monitor) target(api string) (string, bool) {
	host, err := mo.aw.GetHost("soamonitor", "1")
	if err != nil {
		return "", false
	}
	port := "4050"
	if reg := mo.reg(); reg != nil {
		if svc := reg.Services["soamonitor"]; svc != nil && svc.Port != 0 {
			port = strconv.Itoa(svc.Port)
		}
	}
	return "http://" + net.JoinHostPort(host.Host, port) + api, true
}

// Send sends a record, buffered when the monitor configuration asks for it.
func (mo *Monitor) Send(m *monitorRecord, inject string) {
	doc := m.document()
	buffer, _ := m.config["buffer"].(map[string]interface{})
	limit, bufferTime := number(buffer["limit"]), number(buffer["time"])
	if limit != 0 || bufferTime != 0 {
		mo.buffered(doc, limit, bufferTime)
		return
	}
	uri, ok := mo.target("/monitor/item")
	if !ok {
		mo.logger.Debug("Unable to find any healthy host for service soamonitor")
		return
	}
	go mo.post(uri, doc, inject)
}

func (mo *Monitor) post(uri string, payload interface{}, inject string) {
	raw, err := marshalNoEscape(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, uri, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if inject != "" {
		req.Header.Set("soajsinjectobj", inject)
	}
	resp, err := mo.client.Do(req)
	if err != nil {
		mo.logger.Debug("Unable register monitor: " + err.Error())
		return
	}
	resp.Body.Close()
}

func (mo *Monitor) buffered(doc object, limit, bufferTime int) {
	mo.mu.Lock()
	mo.buffer = append(mo.buffer, doc)
	if limit != 0 && len(mo.buffer) >= limit {
		mo.mu.Unlock()
		mo.Flush()
		return
	}
	if bufferTime != 0 && mo.timer == nil {
		mo.timer = time.AfterFunc(time.Duration(bufferTime)*time.Millisecond, func() {
			mo.mu.Lock()
			mo.timer = nil
			mo.mu.Unlock()
			mo.Flush()
		})
	}
	mo.mu.Unlock()
}

// Flush sends the buffered records in one batch.
func (mo *Monitor) Flush() {
	mo.mu.Lock()
	if mo.inProgress || len(mo.buffer) == 0 {
		mo.mu.Unlock()
		return
	}
	mo.inProgress = true
	if mo.timer != nil {
		mo.timer.Stop()
		mo.timer = nil
	}
	items := mo.buffer
	mo.buffer = nil
	mo.mu.Unlock()

	uri, ok := mo.target("/monitor/items")
	mo.mu.Lock()
	mo.inProgress = false
	if !ok {
		mo.buffer = append(items, mo.buffer...)
		mo.mu.Unlock()
		mo.logger.Debug("Monitor buffer flush: Unable to find host for soamonitor")
		return
	}
	mo.mu.Unlock()
	go mo.post(uri, map[string]interface{}{"items": items}, "")
}
