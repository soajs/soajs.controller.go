package middleware

import (
	"regexp"
	"strings"
)

// fromSwagger turns a Swagger 2.0 or OpenAPI 3.x document into tool definitions, as
// soajs.core.libs mcp.fromSwagger does: one per operation on get, post, put and delete, in the
// document's order, in the format the soajs services answer on GET /mcp/tools:
// {m, v, l, inputSchema, fields[, group][, operationId]}. fields says where each argument goes:
// {in: params|query|body, path: [...]}, an empty path being the whole body.
func fromSwagger(doc *omap) []interface{} {
	tools := []interface{}{}
	paths := asOmap(mustGet(doc, "paths"))
	if doc == nil || paths == nil {
		return tools
	}
	swagger, _ := doc.get("swagger")
	openapi, _ := doc.get("openapi")
	if swagger != "2.0" && !strings.HasPrefix(str(openapi), "3.") {
		return tools
	}
	for _, route := range paths.jsKeys() {
		pathItem := asOmap(swDeref(doc, paths.m[route], nil))
		if pathItem == nil || !strings.HasPrefix(route, "/") {
			continue
		}
		for _, method := range []string{"get", "post", "put", "delete"} {
			if op := asOmap(mustGet(pathItem, method)); op != nil {
				tools = append(tools, swBuildTool(doc, method, route, pathItem, op))
			}
		}
	}
	return tools
}

func mustGet(o *omap, k string) interface{} {
	v, _ := o.get(k)
	return v
}

var swDangerous = map[string]bool{"__proto__": true, "constructor": true, "prototype": true}

var swSwagger2SchemaKeys = []string{"type", "format", "items", "enum", "default", "minimum", "maximum",
	"exclusiveMinimum", "exclusiveMaximum", "minLength", "maxLength", "pattern", "minItems",
	"maxItems", "uniqueItems", "multipleOf", "description"}

var swDropKeys = map[string]bool{"xml": true, "externalDocs": true, "discriminator": true, "example": true, "deprecated": true,
	"nullable": true, "readOnly": true, "writeOnly": true, "collectionFormat": true, "allowEmptyValue": true}

// swLookup follows a local reference: "#/definitions/User", "#/components/schemas/User".
func swLookup(doc *omap, ref string) interface{} {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var node interface{} = doc
	for _, part := range strings.Split(ref[2:], "/") {
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		o := asOmap(node)
		if swDangerous[part] || o == nil || !o.has(part) {
			return nil
		}
		node = o.m[part]
	}
	return node
}

// swDeref follows $ref until a real object is reached; a loop is nil.
func swDeref(doc *omap, obj interface{}, seen []string) interface{} {
	for {
		o := asOmap(obj)
		if o == nil {
			return obj
		}
		ref, ok := o.m["$ref"].(string)
		if !ok {
			return obj
		}
		for _, s := range seen {
			if s == ref {
				return nil
			}
		}
		seen = append(seen, ref)
		obj = swLookup(doc, ref)
	}
}

// swResolve resolves the $refs of a schema into a self-contained JSON Schema; a reference back
// to itself is {}. readOnly properties are left out.
func swResolve(doc *omap, schema interface{}, stack []string) interface{} {
	if list, ok := schema.([]interface{}); ok {
		out := make([]interface{}, len(list))
		for i, e := range list {
			out[i] = swResolve(doc, e, stack)
		}
		return out
	}
	s := asOmap(schema)
	if s == nil {
		return schema
	}
	if ref, ok := s.m["$ref"].(string); ok {
		for _, e := range stack {
			if e == ref {
				return newOmap()
			}
		}
		target := asOmap(swLookup(doc, ref))
		if target == nil {
			return newOmap()
		}
		return swResolve(doc, target, append(append([]string(nil), stack...), ref))
	}
	out := newOmap()
	for _, key := range s.jsKeys() {
		if swDangerous[key] || swDropKeys[key] {
			continue
		}
		value := s.m[key]
		props := asOmap(value)
		list, isList := value.([]interface{})
		switch {
		case key == "properties" && props != nil:
			outProps := newOmap()
			for _, prop := range props.jsKeys() {
				if swDangerous[prop] || asOmap(props.m[prop]) == nil {
					continue
				}
				if ps := asOmap(swDeref(doc, props.m[prop], nil)); ps != nil && ps.m["readOnly"] == true {
					continue
				}
				outProps.set(prop, swResolve(doc, props.m[prop], stack))
			}
			out.set("properties", outProps)
		case key == "required" && isList:
			out.set("required", append([]interface{}(nil), list...))
		case props != nil || isList:
			out.set(key, swResolve(doc, value, stack))
		default:
			out.set(key, value)
		}
	}
	if t, ok := s.m["type"].(string); ok && s.m["nullable"] == true {
		out.set("type", []interface{}{t, "null"})
	}
	if out.m["type"] == "file" {
		out.set("type", "string")
	}
	if props := asOmap(mustGet(out, "properties")); props != nil {
		if req, ok := out.m["required"].([]interface{}); ok {
			var kept []interface{}
			for _, name := range req {
				if n, ok := name.(string); ok && props.has(n) {
					kept = append(kept, name)
				}
			}
			if len(kept) == 0 {
				out.del("required")
			} else {
				out.set("required", kept)
			}
		}
	}
	return out
}

// swMergeParameters: operation parameters override path parameters of the same name and place.
func swMergeParameters(doc *omap, pathParams, opParams interface{}) []*omap {
	var list []*omap
	index := map[string]int{}
	for _, params := range []interface{}{pathParams, opParams} {
		arr, ok := params.([]interface{})
		if !ok {
			continue
		}
		for _, p := range arr {
			param := asOmap(swDeref(doc, p, nil))
			name, nameOK := mustGet(param, "name").(string)
			in, inOK := mustGet(param, "in").(string)
			if param == nil || !nameOK || !inOK {
				continue
			}
			key := in + ":" + name
			if i, ok := index[key]; ok {
				list[i] = param
			} else {
				index[key] = len(list)
				list = append(list, param)
			}
		}
	}
	return list
}

func swParamSchema(doc *omap, param *omap) interface{} {
	if s := asOmap(mustGet(param, "schema")); s != nil {
		schema := swResolve(doc, s, nil)
		if o := asOmap(schema); o != nil && jsTruthy(param.m["description"]) && !jsTruthy(o.m["description"]) {
			o.set("description", param.m["description"])
		}
		return schema
	}
	schema := newOmap()
	for _, k := range swSwagger2SchemaKeys {
		if param.has(k) {
			schema.set(k, param.m[k])
		}
	}
	return swResolve(doc, schema, nil)
}

type swBody struct {
	schema   interface{}
	required bool
	name     interface{}
}

// swBodyOf is the body of an operation: Swagger 2 "in: body" or "in: formData" parameters,
// OpenAPI 3 requestBody, JSON first, then form content.
func swBodyOf(doc *omap, params []*omap, op *omap) *swBody {
	for _, p := range params {
		if p.m["in"] == "body" {
			s := mustGet(p, "schema")
			if !jsTruthy(s) {
				s = newOmap()
			}
			return &swBody{schema: swResolve(doc, s, nil), required: p.m["required"] == true, name: p.m["name"]}
		}
	}
	var form []*omap
	for _, p := range params {
		if p.m["in"] == "formData" {
			form = append(form, p)
		}
	}
	if len(form) > 0 {
		props := newOmap()
		var required []interface{}
		for _, p := range form {
			name := str(p.m["name"])
			if swDangerous[name] {
				continue
			}
			props.set(name, swParamSchema(doc, p))
			if p.m["required"] == true {
				required = append(required, name)
			}
		}
		schema := newOmap().set("type", "object").set("properties", props)
		if len(required) > 0 {
			schema.set("required", required)
		}
		return &swBody{schema: schema, required: true, name: "body"}
	}
	rb := asOmap(swDeref(doc, mustGet(op, "requestBody"), nil))
	content := asOmap(mustGet(rb, "content"))
	if rb == nil || content == nil {
		return nil
	}
	types := content.jsKeys()
	pick := ""
	for _, t := range types {
		if strings.Contains(t, "json") {
			pick = t
			break
		}
	}
	if pick == "" {
		for _, t := range types {
			if t == "application/x-www-form-urlencoded" || t == "multipart/form-data" {
				pick = t
				break
			}
		}
	}
	media := asOmap(mustGet(content, pick))
	if pick == "" || media == nil {
		return nil
	}
	s := mustGet(media, "schema")
	if !jsTruthy(s) {
		s = newOmap()
	}
	return &swBody{schema: swResolve(doc, s, nil), required: rb.m["required"] == true, name: "body"}
}

var swPathVar = regexp.MustCompile(`\{[^}]+\}`)

func swBuildTool(doc *omap, method, route string, pathItem, op *omap) *omap {
	params := swMergeParameters(doc, mustGet(pathItem, "parameters"), mustGet(op, "parameters"))
	properties := newOmap()
	fields := newOmap()
	var required []interface{}
	add := func(name string, schema interface{}, where *omap, isRequired bool) {
		if swDangerous[name] || properties.has(name) {
			return
		}
		properties.set(name, schema)
		fields.set(name, where)
		if isRequired {
			required = append(required, name)
		}
	}
	where := func(in string, path ...interface{}) *omap {
		if path == nil {
			path = []interface{}{}
		}
		return newOmap().set("in", in).set("path", path)
	}
	// path first, then query, then body: on a name clash the earlier one wins
	for _, p := range params {
		if p.m["in"] == "path" {
			name := str(p.m["name"])
			add(name, swParamSchema(doc, p), where("params", name), true)
		}
	}
	for _, p := range params {
		if p.m["in"] == "query" {
			name := str(p.m["name"])
			add(name, swParamSchema(doc, p), where("query", name), p.m["required"] == true)
		}
	}
	if body := swBodyOf(doc, params, op); body != nil {
		schema := asOmap(body.schema)
		props := asOmap(mustGet(schema, "properties"))
		t, _ := schema.get("type")
		if schema != nil && props != nil && (!jsTruthy(t) || t == "object") {
			bodyRequired, _ := schema.m["required"].([]interface{})
			for _, prop := range props.jsKeys() {
				isReq := false
				if body.required {
					for _, r := range bodyRequired {
						if r == prop {
							isReq = true
						}
					}
				}
				add(prop, props.m[prop], where("body", prop), isReq)
			}
		} else {
			name := "body"
			if jsTruthy(body.name) {
				name = jsToString(body.name)
			}
			var s interface{} = newOmap()
			if schema != nil {
				s = schema
			}
			add(name, s, where("body"), body.required)
		}
	}
	// a path template variable with no parameter declared for it
	for _, v := range swPathVar.FindAllString(route, -1) {
		name := v[1 : len(v)-1]
		if !fields.has(name) {
			add(name, newOmap().set("type", "string"), where("params", name), true)
		}
	}
	inputSchema := newOmap().set("type", "object").set("properties", properties)
	if len(required) > 0 {
		inputSchema.set("required", required)
	}
	// summary || description || "METHOD /route"
	label := strings.ToUpper(method) + " " + route
	if jsTruthy(op.m["summary"]) {
		label = jsToString(op.m["summary"])
	} else if jsTruthy(op.m["description"]) {
		label = jsToString(op.m["description"])
	}
	tool := newOmap().
		set("m", method).
		set("v", swPathVar.ReplaceAllStringFunc(route, func(s string) string { return ":" + s[1:len(s)-1] })).
		set("l", label).
		set("inputSchema", inputSchema).
		set("fields", fields)
	if tags, ok := op.m["tags"].([]interface{}); ok && len(tags) > 0 {
		if t, ok := tags[0].(string); ok {
			tool.set("group", t)
		}
	}
	if id, ok := op.m["operationId"].(string); ok {
		tool.set("operationId", id)
	}
	return tool
}
