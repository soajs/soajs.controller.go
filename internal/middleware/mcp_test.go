package middleware

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

// mcpVectors are outputs of the Node.js functions (testdata/mcp_vectors.json, made with Node.js
// 24 from soajs.core.libs mcp.fromSwagger and mw/soajsRoutes/mcp/tools.js buildCall and toolName).
func mcpVectors(t *testing.T) *omap {
	raw, err := os.ReadFile("testdata/mcp_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	v, err := parseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return asOmap(v)
}

func TestFromSwagger_NodeVectors(t *testing.T) {
	for i, c := range mcpVectors(t).m["swagger"].([]interface{}) {
		vector := asOmap(c)
		got := jsStringify(fromSwagger(asOmap(vector.m["doc"])))
		want := jsStringify(vector.m["tools"])
		if got != want {
			t.Errorf("document %d\n got  %s\n want %s", i, got, want)
		}
	}
}

func TestBuildCall_NodeVectors(t *testing.T) {
	v := mcpVectors(t)
	tool := asOmap(v.m["tool"])
	for i, c := range v.m["calls"].([]interface{}) {
		vector := asOmap(c)
		want := asOmap(vector.m["call"])
		got := mcpBuildCall(tool, vector.m["args"])
		if e, ok := want.m["error"]; ok {
			if got.err != e {
				t.Errorf("call %d: error %q, want %q", i, got.err, e)
			}
			continue
		}
		if got.err != "" || got.path != want.m["path"] || got.query != want.m["query"] {
			t.Errorf("call %d: %q %q %q, want %v %v", i, got.err, got.path, got.query, want.m["path"], want.m["query"])
		}
		wantBody, hasBody := want.get("body")
		if hasBody != got.hasBody || (hasBody && jsStringify(got.body) != jsStringify(wantBody)) {
			t.Errorf("call %d: body %s, want %s", i, jsStringify(got.body), jsStringify(wantBody))
		}
	}
}

func TestToolName_NodeVectors(t *testing.T) {
	want := mcpVectors(t).m["names"].([]interface{})
	inputs := []struct {
		service string
		tool    *omap
	}{
		{"svc", newOmap().set("m", "get").set("v", "/a/:b")},
		{"svc", newOmap().set("m", "get").set("v", "/a/:b")},
		{"svc", newOmap().set("m", "get").set("v", "/a/:b")},
		{strings.Repeat("x", 70), newOmap().set("m", "get").set("v", "/")},
		{strings.Repeat("x", 70), newOmap().set("m", "get").set("v", "/")},
		{"svc", newOmap().set("m", "post").set("v", "/-x-/").set("operationId", "__op ids__")},
	}
	taken := map[string]bool{}
	for i, in := range inputs {
		if got := mcpToolName(in.service, in.tool, taken); got != want[i] {
			t.Errorf("%d: %q, want %q", i, got, want[i])
		}
	}
}

func TestOrderedValues(t *testing.T) {
	v, err := parseJSON([]byte(`{"b":1,"2":true,"a":[1.50,"x\u2028"],"10":null,"b":2e21}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := jsStringify(v); got != `{"2":true,"10":null,"b":2e+21,"a":[1.5,"x`+"\u2028"+`"]}` {
		t.Errorf("stringify %s", got)
	}
	if _, err := parseJSON([]byte(`{} x`)); err == nil {
		t.Error("trailing data")
	}
	y, err := parseYAML("b: 1\na: [x, 2.5]\nc: &x {k: v}\nd: *x\ne:\n  <<: *x\n  z: 0x10\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := jsStringify(y); got != `{"b":1,"a":["x",2.5],"c":{"k":"v"},"d":{"k":"v"},"e":{"k":"v","z":16}}` {
		t.Errorf("yaml %s", got)
	}
	for f, want := range map[float64]string{1e21: "1e+21", 1.5e-7: "1.5e-7", 0.000001: "0.000001", 123456789012: "123456789012", -0.5: "-0.5"} {
		if got := jsFormatNumber(f); got != want {
			t.Errorf("%v: %s, want %s", f, got, want)
		}
	}
}

func TestMCPServe_Protocol(t *testing.T) {
	reg := &mockRegistryProxy{data: &types.RegistryData{CustomRegistry: map[string]interface{}{
		"gateway": map[string]interface{}{"value": map[string]interface{}{"mcp": map[string]interface{}{"enabled": true}}}}}}
	m := NewMCP(reg, GatewayService{Name: "controller", Version: "1"})
	ctx := &SOAJSContext{Controller: &ControllerContext{ServiceParams: &types.ServiceParams{}}, Key: mtKey("k", "P")}
	cases := []struct {
		method, body string
		status       int
		want         string
	}{
		{"GET", "", 405, `{"jsonrpc":"2.0","id":null,"error":{"code":-32600,"message":"Use POST"}}`},
		{"POST", "{", 400, `"code":-32700`},
		{"POST", "[]", 400, `Batches are not supported`},
		{"POST", `{"jsonrpc":"2.0","id":"x","method":1}`, 400, `{"jsonrpc":"2.0","id":"x","error":{"code":-32600,"message":"Invalid request"}}`},
		{"POST", `0`, 400, `{"jsonrpc":"2.0","id":0,"error"`},
		{"POST", `"s"`, 400, `{"jsonrpc":"2.0","id":null,"error"`},
		{"POST", `{"jsonrpc":"2.0","method":"notifications/initialized"}`, 202, ``},
		{"POST", `{"jsonrpc":"2.0","id":1.0,"method":"ping"}`, 200, `{"jsonrpc":"2.0","id":1,"result":{}}`},
		{"POST", `{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"protocolVersion":"1999"}}`, 200, `"protocolVersion":"2025-06-18"`},
		{"POST", `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":[]}`, 200, `params.name is required`},
		{"POST", strings.Repeat(" ", mcpMaxBody+1), 413, `Request larger than 1048576 bytes`},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		m.serve(rec, httptest.NewRequest(c.method, "/soajs/mcp", strings.NewReader(c.body)), ctx)
		if rec.Code != c.status || !strings.Contains(rec.Body.String(), c.want) {
			t.Errorf("%s %.40q: %d %s", c.method, c.body, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Content-Length") == "" {
			t.Errorf("%.40q: no Content-Length", c.body)
		}
	}
	// not enabled
	reg.data.CustomRegistry = map[string]interface{}{}
	rec := httptest.NewRecorder()
	m.serve(rec, httptest.NewRequest("POST", "/soajs/mcp", nil), ctx)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"codes":[190]`) {
		t.Errorf("disabled: %d %s", rec.Code, rec.Body.String())
	}
}

func TestMCPAuthHelpers(t *testing.T) {
	auth := map[string]interface{}{
		"scheme":    "ftp",
		"default":   map[string]interface{}{"authorization_endpoint": "a", "token_endpoint": "t"},
		"byHost":    map[string]interface{}{"h.example": map[string]interface{}{"authorization_endpoint": "a"}},
		"byProduct": map[string]interface{}{"P": map[string]interface{}{"authorization_endpoint": "pa", "token_endpoint": "pt"}},
	}
	r := httptest.NewRequest("GET", "/x", nil)
	r.Host = "H.example"
	r.Header.Set("X-Forwarded-Proto", "https, http")
	if got := requestOrigin(r, auth); got != "https://h.example" {
		t.Errorf("origin %s", got)
	}
	prov := &mockProvision{keys: map[string]*types.Key{"k": mtKey("k", "P")}}
	prov.keys["k"].Application.Product = "P"
	if m := resolveEntry(r, prov, auth, "k"); m == nil || m.id != "product/P" {
		t.Errorf("by product: %+v", m)
	}
	// the host entry has no token endpoint: the default
	if m := resolveEntry(r, prov, auth, ""); m == nil || m.id != "default" {
		t.Errorf("by host: %+v", m)
	}
	if entryByID(r, auth, "product/P") == nil || entryByID(r, auth, "host") != nil || entryByID(r, auth, "nope") != nil {
		t.Error("entry by id")
	}
	tok := func(key interface{}) *types.AccessToken {
		return &types.AccessToken{User: map[string]interface{}{"restrictedTo": map[string]interface{}{"key": key}}, Expires: time.Now().Add(time.Hour)}
	}
	if tokenKey(tok("a")) != "a" || tokenKey(tok([]interface{}{"a"})) != "a" || tokenKey(tok([]interface{}{"a", "b"})) != "" || tokenKey(&types.AccessToken{}) != "" {
		t.Error("token key")
	}
	if got := firstQueryValue("?key=a+b&key=c", "key"); got != "a b" {
		t.Errorf("first value %q", got)
	}
}

func TestMCPToolsFromAnswer(t *testing.T) {
	parse := func(s string) interface{} {
		v, err := parseJSON([]byte(s))
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	if tools, _ := mcpToolsFromAnswer(parse(`{"result":true,"data":{"tools":[{"m":"get","v":"/x"}]}}`)); len(tools) != 1 {
		t.Error("tools")
	}
	if tools, _ := mcpToolsFromAnswer(parse(`{"result":false,"data":{"tools":[{}]}}`)); tools != nil {
		t.Error("result false")
	}
	tools, err := mcpToolsFromAnswer(parse(`{"result":true,"data":{"swagger":"swagger: '2.0'\npaths:\n  /y:\n    delete: {}\n"}}`))
	if err != nil || len(tools) != 1 || asOmap(tools[0]).m["v"] != "/y" {
		t.Errorf("swagger %v %v", tools, err)
	}
	if _, err := mcpToolsFromAnswer(parse(`{"result":true,"data":{"swagger":"a: [b"}}`)); err == nil {
		t.Error("invalid YAML")
	}
	b, _ := json.Marshal(newOmap().set("z", 1.0).set("a", "x"))
	if string(b) != `{"z":1,"a":"x"}` {
		t.Errorf("marshal %s", b)
	}
}
