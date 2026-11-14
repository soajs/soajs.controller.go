package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"github.com/soajs/soajs.controller.go/pkg/version"
)

const (
	mcpMaxBody     = 1024 * 1024
	mcpMaxResponse = 5 * 1024 * 1024
	mcpNameMax     = 64
	mcpIdemArg     = "_idempotencyKey"

	rpcParse          = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

var mcpProtocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// the request headers a tool call passes on; not idempotency-key, a tool call carries its own
var mcpForwardHeaders = []string{"key", "authorization", "access_token", "device-id", "user-agent", "accept-language"}

// mcpConfig is the custom registry gateway mcp, with its defaults.
type mcpConfig struct {
	ttl, errorTTL, timeout time.Duration
	maxTools               int
}

// mcpConfigOf is the mcp configuration when mcp.enabled is true: ttl (300000 ms), errorTtl
// (30000 ms), timeout (10000 ms) and maxTools (300), each kept when a positive number.
func mcpConfigOf(regData *types.RegistryData) *mcpConfig {
	mcp := mcpEntry(regData)
	if mcp == nil || mcp["enabled"] != true {
		return nil
	}
	num := func(k string, def float64) float64 {
		if f, ok := mcp[k].(float64); ok && f > 0 {
			return f
		}
		return def
	}
	return &mcpConfig{
		ttl:      time.Duration(num("ttl", 300000)) * time.Millisecond,
		errorTTL: time.Duration(num("errorTtl", 30000)) * time.Millisecond,
		timeout:  time.Duration(num("timeout", 10000)) * time.Millisecond,
		maxTools: int(num("maxTools", 300)),
	}
}

func mcpEntry(regData *types.RegistryData) map[string]interface{} {
	if regData == nil {
		return nil
	}
	gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	mcp, _ := value["mcp"].(map[string]interface{})
	return mcp
}

// MCP is /soajs/mcp (mw/soajsRoutes/mcp): MCP over HTTP, JSON-RPC 2.0, one JSON answer per
// request. The tools are the APIs the caller may use, described by each service on its
// maintenance GET /mcp/tools (soajs tools, or a Swagger / OpenAPI document), cached per service
// and version; a tool call is the API call it stands for, made through the gateway's own port.
type MCP struct {
	reg    registry.Registry
	gw     GatewayService
	client *http.Client

	mu    sync.Mutex
	cache map[string]mcpCached
}

type mcpCached struct {
	expires time.Time
	tools   []*omap
}

// NewMCP creates the /soajs/mcp handler.
func NewMCP(reg registry.Registry, gw GatewayService) *MCP {
	return &MCP{reg: reg, gw: gw, client: &http.Client{Transport: proxyTransport}, cache: map[string]mcpCached{}}
}

func rpcError(id interface{}, code int, message string) *omap {
	return newOmap().set("jsonrpc", "2.0").set("id", id).
		set("error", newOmap().set("code", json.Number(strconv.Itoa(code))).set("message", message))
}

func rpcResult(id interface{}, result interface{}) *omap {
	return newOmap().set("jsonrpc", "2.0").set("id", id).set("result", result)
}

// mcpSend writes a JSON answer with its Content-Length; nil writes an empty body.
func mcpSend(w http.ResponseWriter, status int, body *omap, headers map[string]string) {
	text := ""
	if body != nil {
		text = jsStringify(body)
		w.Header().Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		w.Header().Set(k, v)
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(text)))
	w.WriteHeader(status)
	_, _ = io.WriteString(w, text)
}

// serve answers /soajs/mcp after mt: 404 (190) when mcp is not enabled, 405 for any method but
// POST; a body over 1 MB answers 413, one that is not JSON 400, a batch 400, an invalid request
// 400, a notification 202; otherwise the method's answer.
func (m *MCP) serve(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext) {
	config := mcpConfigOf(m.reg.Get())
	if config == nil {
		_ = WriteError(w, http.StatusNotFound, 190, "MCP is not enabled in this environment.")
		return
	}
	if r.Method != http.MethodPost {
		mcpSend(w, http.StatusMethodNotAllowed, rpcError(nil, rpcInvalidRequest, "Use POST"), map[string]string{"Allow": "POST"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, mcpMaxBody+1))
	if err != nil || len(raw) > mcpMaxBody {
		mcpSend(w, http.StatusRequestEntityTooLarge, rpcError(nil, rpcInvalidRequest, "Request larger than "+strconv.Itoa(mcpMaxBody)+" bytes"), nil)
		return
	}
	message, err := parseJSON([]byte(utf8Body(raw)))
	if err != nil {
		mcpSend(w, http.StatusBadRequest, rpcError(nil, rpcParse, "Parse error"), nil)
		return
	}
	if _, isList := message.([]interface{}); isList {
		mcpSend(w, http.StatusBadRequest, rpcError(nil, rpcInvalidRequest, "Batches are not supported"), nil)
		return
	}
	msg := asOmap(message)
	method, isString := mustGet(msg, "method").(string)
	if msg == nil || msg.m["jsonrpc"] != "2.0" || !isString {
		// message && message.id
		var id interface{}
		if msg != nil {
			id = msg.m["id"]
		} else if !jsTruthy(message) {
			id = message
		}
		mcpSend(w, http.StatusBadRequest, rpcError(id, rpcInvalidRequest, "Invalid request"), nil)
		return
	}
	if !msg.has("id") {
		mcpSend(w, http.StatusAccepted, nil, nil)
		return
	}
	mcpSend(w, http.StatusOK, m.handle(r, soajsCtx, config, msg, method), nil)
}

func (m *MCP) handle(r *http.Request, soajsCtx *SOAJSContext, config *mcpConfig, msg *omap, method string) *omap {
	id := msg.m["id"]
	params := asOmap(msg.m["params"])
	switch method {
	case "initialize":
		protocol := mcpProtocolVersions[0]
		for _, v := range mcpProtocolVersions {
			if mustGet(params, "protocolVersion") == v {
				protocol = v
			}
		}
		return rpcResult(id, newOmap().
			set("protocolVersion", protocol).
			set("capabilities", newOmap().set("tools", newOmap().set("listChanged", false))).
			set("serverInfo", newOmap().set("name", "soajs-gateway").set("version", version.Get())).
			set("instructions", "The tools are the APIs this tenant's package and the signed-in user's ACL allow. A tool call is processed as the API call it stands for."))
	case "ping":
		return rpcResult(id, newOmap())
	case "tools/list":
		tools, _ := m.listTools(soajsCtx, config)
		list := make([]interface{}, len(tools))
		for i, t := range tools {
			list[i] = t
		}
		return rpcResult(id, newOmap().set("tools", list))
	case "tools/call":
		name, ok := mustGet(params, "name").(string)
		if !ok {
			return rpcError(id, rpcInvalidParams, "params.name is required")
		}
		_, index := m.listTools(soajsCtx, config)
		entry, ok := index[name]
		if !ok {
			return rpcError(id, rpcInvalidParams, "Unknown tool: "+name)
		}
		args, _ := params.get("arguments")
		return rpcResult(id, m.callTool(r, soajsCtx, config, entry, args))
	}
	return rpcError(id, rpcMethodNotFound, "Method not found: "+method)
}

// mcpEntryRef is what a tool name calls: the service, its version and the tool.
type mcpEntryRef struct {
	service, version string
	tool             *omap
}

// listTools are the tools the caller may use: for each service of the caller's ACL but the
// gateway, in order, the tools of its latest allowed version that the ACL allows, at most
// maxTools.
func (m *MCP) listTools(soajsCtx *SOAJSContext, config *mcpConfig) ([]*omap, map[string]mcpEntryRef) {
	acl, _, _ := computeACL(soajsCtx)
	finalACL, restricted := finalACLOf(acl, soajsCtx.Log)
	var services []string
	for s := range finalACL {
		if s != m.gw.Name {
			services = append(services, s)
		}
	}
	sort.Slice(services, func(i, j int) bool { return jsLess(services[i], services[j]) })
	lists := make([][]*omap, len(services))
	var wg sync.WaitGroup
	for i, s := range services {
		wg.Add(1)
		go func(i int, s string) {
			defer wg.Done()
			lists[i] = m.serviceTools(soajsCtx, s, finalACL[s].(map[string]interface{})["version"].(string), config)
		}(i, s)
	}
	wg.Wait()
	regData := m.reg.Get()
	var tools []*omap
	index := map[string]mcpEntryRef{}
	taken := map[string]bool{}
	for i, s := range services {
		serviceACL := finalACL[s].(map[string]interface{})
		for _, tool := range lists[i] {
			if len(tools) >= config.maxTools || !mcpAllowed(serviceACL, tool, restricted[s]) {
				continue
			}
			method, route := str(tool.m["m"]), str(tool.m["v"])
			name := mcpToolName(s, tool, taken)
			description := strings.ToUpper(method) + " " + route
			if jsTruthy(tool.m["l"]) {
				description = jsToString(tool.m["l"])
			}
			inputSchema := asOmap(tool.m["inputSchema"])
			if inputSchema == nil || inputSchema.m["type"] != "object" {
				inputSchema = newOmap().set("type", "object").set("properties", newOmap())
			}
			if mcpTakesIdemArg(tool) && mcpIdempotent(regData, s, tool) {
				props := asOmap(inputSchema.m["properties"])
				if props == nil {
					props = newOmap()
				}
				props = props.copy()
				props.set(mcpIdemArg, newOmap().set("type", "string").set("format", "uuid").
					set("description", "Optional UUID v4. Send the same value when retrying this exact call, so it runs once."))
				inputSchema = inputSchema.copy().set("properties", props)
			}
			tools = append(tools, newOmap().
				set("name", name).
				set("title", description).
				set("description", description+" ["+s+": "+strings.ToUpper(method)+" "+route+"]").
				set("inputSchema", inputSchema).
				set("annotations", newOmap().
					set("readOnlyHint", method == "get").
					set("destructiveHint", method == "delete").
					set("openWorldHint", false)))
			index[name] = mcpEntryRef{service: s, version: serviceACL["version"].(string), tool: tool}
		}
	}
	return tools, index
}

// serviceTools are the tools of a service version, cached for ttl, errorTtl when the service
// did not answer. A service without a port has none.
func (m *MCP) serviceTools(soajsCtx *SOAJSContext, service, ver string, config *mcpConfig) []*omap {
	key := service + "@" + ver
	m.mu.Lock()
	if c, ok := m.cache[key]; ok && c.expires.After(time.Now()) {
		m.mu.Unlock()
		return c.tools
	}
	m.mu.Unlock()
	store := func(tools []*omap, ttl time.Duration) []*omap {
		m.mu.Lock()
		m.cache[key] = mcpCached{expires: time.Now().Add(ttl), tools: tools}
		m.mu.Unlock()
		return tools
	}
	regData := m.reg.Get()
	if regData == nil || regData.Services[service] == nil || regData.Services[service].Port == 0 {
		return nil
	}
	var host string
	var err error
	if soajsCtx.Awareness != nil {
		host, err = soajsCtx.Awareness.GetHost(service, ver)
	}
	if soajsCtx.Awareness == nil || err != nil || host == "" {
		soajsCtx.Log.Error("MCP: no host for [" + key + "]")
		return store(nil, config.errorTTL)
	}
	uri := "http://" + host + ":" + strconv.Itoa(mcpMaintenancePort(regData, service)) + "/mcp/tools"
	resp, err := m.request(context.Background(), uri, http.MethodGet, nil, nil, config.timeout)
	if err != nil {
		soajsCtx.Log.Error("MCP: could not get the tools of [" + key + "]: " + err.Error())
		return store(nil, config.errorTTL)
	}
	var tools []interface{}
	if resp.status == 200 {
		tools, err = mcpToolsFromAnswer(resp.json)
		if err != nil {
			soajsCtx.Log.Error("MCP: the tools of [" + key + "] could not be read: " + err.Error())
		}
	}
	var out []*omap
	for _, t := range tools {
		tool := asOmap(t)
		if _, mOK := mustGet(tool, "m").(string); !mOK {
			continue
		}
		if _, vOK := mustGet(tool, "v").(string); !vOK {
			continue
		}
		out = append(out, tool)
	}
	return store(out, config.ttl)
}

// mcpMaintenancePort is the port a service answers its maintenance routes on: its port for
// "inherit", else its port plus maintenanceInc (1000).
func mcpMaintenancePort(regData *types.RegistryData, service string) int {
	svc := regData.Services[service]
	if svc.Maintenance != nil && svc.Maintenance.Port.Type == "inherit" {
		return svc.Port
	}
	inc := 0
	if regData.ServiceConfig != nil {
		inc = regData.ServiceConfig.Ports.MaintenanceInc
	}
	if inc == 0 {
		inc = 1000
	}
	return svc.Port + inc
}

// mcpToolsFromAnswer reads the tools of a service's answer {result: true, data}: data.tools, or
// data.swagger, a Swagger / OpenAPI document (an object, or YAML text).
func mcpToolsFromAnswer(answer interface{}) ([]interface{}, error) {
	a := asOmap(answer)
	if a == nil || a.m["result"] != true {
		return nil, nil
	}
	data := asOmap(a.m["data"])
	if data == nil {
		return nil, nil
	}
	if tools, ok := data.m["tools"].([]interface{}); ok {
		return tools, nil
	}
	doc := data.m["swagger"]
	if text, ok := doc.(string); ok {
		parsed, err := parseYAML(text)
		if err != nil {
			return nil, err
		}
		doc = parsed
	}
	if d := asOmap(doc); d != nil {
		return fromSwagger(d), nil
	}
	return nil, nil
}

var (
	mcpNameInvalid = regexp.MustCompile(`[^A-Za-z0-9_-]+`)
	mcpUnderscores = regexp.MustCompile(`_+`)
)

// mcpToolName is a name agents accept: [A-Za-z0-9_-], at most 64 characters, unique.
func mcpToolName(service string, tool *omap, taken map[string]bool) string {
	id := str(tool.m["m"]) + str(tool.m["v"])
	if op, ok := tool.m["operationId"].(string); ok && op != "" {
		id = op
	}
	base := mcpNameInvalid.ReplaceAllString(service+"_"+id, "_")
	base = strings.Trim(mcpUnderscores.ReplaceAllString(base, "_"), "_")
	if len(base) > mcpNameMax {
		base = base[:mcpNameMax]
	}
	name := base
	for n := 2; taken[name]; n++ {
		suffix := "_" + strconv.Itoa(n)
		cut := mcpNameMax - len(suffix)
		if cut > len(base) {
			cut = len(base)
		}
		name = base[:cut] + suffix
	}
	taken[name] = true
	return name
}

// mcpAllowed: a restricted service allows only the APIs it lists for the method.
func mcpAllowed(serviceACL map[string]interface{}, tool *omap, restricted bool) bool {
	if !restricted {
		return true
	}
	method := str(tool.m["m"])
	list, ok := serviceACL[method].([]string)
	if !ok && method == "delete" {
		list, ok = serviceACL["del"].([]string)
	}
	if !ok {
		return false
	}
	for _, api := range list {
		if api == str(tool.m["v"]) {
			return true
		}
	}
	return false
}

// mcpIdempotent: the gateway applies idempotency to the tool's API (not a get).
func mcpIdempotent(regData *types.RegistryData, service string, tool *omap) bool {
	method := str(tool.m["m"])
	if method == "get" || regData == nil {
		return false
	}
	gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
	value, _ := gateway["value"].(map[string]interface{})
	idem, _ := value["idempotency"].(map[string]interface{})
	services, _ := idem["services"].(map[string]interface{})
	svc, _ := services[service].(map[string]interface{})
	if svc == nil || !truthy(svc["enabled"]) {
		return false
	}
	if apis, has := svc["apis"]; has && truthy(apis) {
		list, _ := apis.([]interface{})
		return matchesIdempotentAPI(strings.ToUpper(method), str(tool.m["v"]), list)
	}
	return true
}

// mcpTakesIdemArg: a tool call can carry its idempotency key: not a get, and no field of that
// name.
func mcpTakesIdemArg(tool *omap) bool {
	return str(tool.m["m"]) != "get" && !asOmap(tool.m["fields"]).has(mcpIdemArg)
}

type mcpCall struct {
	path, query string
	body        interface{}
	hasBody     bool
	err         string
}

var mcpParamRe = regexp.MustCompile(`[:*]([A-Za-z0-9_]+)`)
var mcpOptionalRe = regexp.MustCompile(`\{([^}]*)\}`)

// mcpBuildCall is the API call of a tool call: the path with its params, the query string and the
// body, from the tool's fields.
func mcpBuildCall(tool *omap, args interface{}) mcpCall {
	a := asOmap(args)
	if a == nil {
		a = newOmap()
	}
	fields := asOmap(tool.m["fields"])
	params := newOmap()
	var query [][2]string
	var call mcpCall
	for _, name := range fields.jsKeys() {
		if !a.has(name) {
			continue
		}
		where := asOmap(fields.m[name])
		path := []string{name}
		if p, ok := mustGet(where, "path").([]interface{}); ok {
			path = make([]string, len(p))
			for i, e := range p {
				path[i] = jsToString(e)
			}
		}
		value := a.m[name]
		switch mustGet(where, "in") {
		case "params":
			k := name
			if len(path) > 0 && path[0] != "" {
				k = path[0]
			}
			params.set(k, value)
		case "query":
			k := strings.Join(path, ".")
			if k == "" {
				k = name
			}
			if list, ok := value.([]interface{}); ok {
				for _, v := range list {
					query = append(query, [2]string{k, mcpQueryValue(v, true)})
				}
			} else {
				query = append(query, [2]string{k, mcpQueryValue(value, false)})
			}
		case "body":
			if len(path) == 0 {
				call.body, call.hasBody = value, true
				continue
			}
			body := asOmap(call.body)
			if body == nil {
				body = newOmap()
				call.body, call.hasBody = body, true
			}
			node := body
			for _, p := range path[:len(path)-1] {
				next := asOmap(node.m[p])
				if next == nil {
					next = newOmap()
					node.set(p, next)
				}
				node = next
			}
			node.set(path[len(path)-1], value)
		}
	}
	present := func(n string) bool {
		v, ok := params.get(n)
		return ok && v != nil
	}
	missing := ""
	fill := func(segment string) string {
		return mcpParamRe.ReplaceAllStringFunc(segment, func(all string) string {
			n := all[1:]
			if !present(n) {
				if missing == "" {
					missing = n
				}
				return all
			}
			return escapeComponent(jsToString(params.m[n]))
		})
	}
	path := mcpOptionalRe.ReplaceAllStringFunc(str(tool.m["v"]), func(all string) string {
		inner := all[1 : len(all)-1]
		for _, n := range mcpParamRe.FindAllString(inner, -1) {
			if !present(n[1:]) {
				return ""
			}
		}
		return fill(inner)
	})
	path = fill(path)
	if missing != "" {
		return mcpCall{err: "Missing path parameter: " + missing}
	}
	call.path = path
	if len(query) > 0 {
		parts := make([]string, len(query))
		for i, q := range query {
			parts[i] = formEncode(q[0]) + "=" + formEncode(q[1])
		}
		call.query = "?" + strings.Join(parts, "&")
	}
	return call
}

// mcpQueryValue is a query value as a tool call sends it: an object as JSON, else String().
func mcpQueryValue(v interface{}, inList bool) string {
	switch v.(type) {
	case *omap, []interface{}:
		return jsStringify(v)
	case nil:
		if inList {
			return "null"
		}
	}
	return jsToString(v)
}

// formEncode is the application/x-www-form-urlencoded serializer of URLSearchParams.
func formEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == ' ':
			b.WriteByte('+')
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '*' || c == '-' || c == '.' || c == '_':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

var mcpBearer = regexp.MustCompile(`(?i)^Bearer\s+(\S+)`)

// callTool calls the tool's API through the gateway's own port with the caller's credentials,
// so it goes through every check an API call does. A write without _idempotencyKey gets a new
// key; one that is not a UUID v4 is refused.
func (m *MCP) callTool(r *http.Request, soajsCtx *SOAJSContext, config *mcpConfig, entry mcpEntryRef, args interface{}) *omap {
	failed := func(text string) *omap {
		return newOmap().set("content", []interface{}{newOmap().set("type", "text").set("text", text)}).set("isError", true)
	}
	call := mcpBuildCall(entry.tool, args)
	if call.err != "" {
		return failed(call.err)
	}
	headers := map[string]string{}
	for _, h := range mcpForwardHeaders {
		if v := r.Header.Get(h); v != "" {
			headers[h] = v
		}
	}
	if mcpTakesIdemArg(entry.tool) {
		given, has := asOmap(args).get(mcpIdemArg)
		if has && given != nil && given != "" {
			s, ok := given.(string)
			if !ok || !uuidV4.MatchString(s) {
				return failed(mcpIdemArg + " must be a UUID v4.")
			}
			headers["idempotency-key"] = s
		} else {
			headers["idempotency-key"] = newUUIDv4()
		}
	}
	params := soajsCtx.Controller.ServiceParams
	if headers["access_token"] == "" && params.AccessToken != "" {
		headers["access_token"] = params.AccessToken
	}
	if headers["access_token"] == "" && headers["authorization"] != "" {
		if mt := mcpBearer.FindStringSubmatch(headers["authorization"]); mt != nil {
			headers["access_token"] = mt[1]
		}
	}
	if headers["access_token"] != "" {
		delete(headers, "authorization")
	}
	if ip := getClientIP(r); ip != "" {
		if f := r.Header.Get("X-Forwarded-For"); f != "" {
			headers["x-forwarded-for"] = f
		} else {
			headers["x-forwarded-for"] = ip
		}
	}
	port := 0
	if regData := m.reg.Get(); regData != nil && regData.Services[m.gw.Name] != nil {
		port = regData.Services[m.gw.Name].Port
	}
	method := strings.ToUpper(str(entry.tool.m["m"]))
	var body interface{}
	hasBody := false
	if method != http.MethodGet && call.hasBody && call.body != nil {
		body, hasBody = call.body, true
	}
	uri := "http://127.0.0.1:" + strconv.Itoa(port) + "/" + entry.service + "/v" + entry.version + call.path + call.query
	var payload *string
	if hasBody {
		p := jsStringify(body)
		payload = &p
	}
	resp, err := m.request(r.Context(), uri, method, headers, payload, config.timeout)
	if err != nil {
		soajsCtx.Log.Error("MCP: call of [" + entry.service + "@" + entry.version + " " + str(entry.tool.m["m"]) + " " + str(entry.tool.m["v"]) + "] failed: " + err.Error())
		return failed("The call failed: " + err.Error())
	}
	jsonv := resp.json
	jm := asOmap(jsonv)
	isError := resp.status >= 400 || (jsTruthy(jsonv) && jm != nil && jm.m["result"] == false)
	payloadV := jsonv
	if jsTruthy(jsonv) && !isError && jm.has("data") {
		payloadV = jm.m["data"]
	} else if jsTruthy(jsonv) && isError && jm != nil && jsTruthy(jm.m["errors"]) {
		payloadV = jm.m["errors"]
	}
	text := resp.text
	if payloadV != nil {
		text = jsStringify(payloadV)
	}
	result := newOmap().set("content", []interface{}{newOmap().set("type", "text").set("text", text)}).set("isError", isError)
	if p := asOmap(payloadV); !isError && p != nil {
		result.set("structuredContent", p)
	}
	return result
}

type mcpResponse struct {
	status int
	text   string
	json   interface{}
}

var errMCPTimeout = errors.New("Request timed out")

// request sends a request and reads its answer, at most 5 MB; it fails after timeout without
// traffic.
func (m *MCP) request(ctx context.Context, uri, method string, headers map[string]string, payload *string, timeout time.Duration) (*mcpResponse, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	timedOut := false
	var mu sync.Mutex
	idle := newIdleTimer(timeout, func() {
		mu.Lock()
		timedOut = true
		mu.Unlock()
		cancel()
	})
	defer idle.stop()
	var body io.Reader
	if payload != nil {
		body = strings.NewReader(*payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, uri, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if payload != nil {
		req.Header.Set("content-type", "application/json")
		req.ContentLength = int64(len(*payload))
	}
	wrap := func(err error) error {
		mu.Lock()
		defer mu.Unlock()
		if timedOut {
			return errMCPTimeout
		}
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return nil, wrap(err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(idleReader{resp.Body, idle}, mcpMaxResponse+1))
	if n > mcpMaxResponse {
		return nil, errors.New("Response larger than " + strconv.Itoa(mcpMaxResponse) + " bytes")
	}
	if err != nil {
		return nil, wrap(err)
	}
	text := utf8Body(buf.Bytes())
	out := &mcpResponse{status: resp.StatusCode, text: text}
	if text != "" {
		if v, err := parseJSON([]byte(text)); err == nil {
			out.json = v
		}
	}
	return out, nil
}

// mcpRoute prepares /soajs/mcp: 132 without a key; otherwise an API of the gateway, answered by
// serve after mt.
func (m *MCP) route(w http.ResponseWriter, r *http.Request, soajsCtx *SOAJSContext) bool {
	if r.Header.Get("key") == "" {
		WriteCode(w, 132)
		return false
	}
	mimicGatewayAPI(soajsCtx.Controller.ServiceParams, m.gw, "/soajs/mcp")
	soajsCtx.Controller.Gotoservice = func(w http.ResponseWriter, r *http.Request) {
		m.serve(w, r, soajsCtx)
	}
	return true
}
