package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// lastSeenTimeout is the timeout of a notification (lib/request.js httpRequest).
const lastSeenTimeout = 8 * time.Second

var lastSeenClient = &http.Client{Timeout: lastSeenTimeout}

// LastSeen notifies that a logged-in user was active, as the Node.js lastSeen middleware does
// (mw/lastSeen): after the request is passed on, when the custom registry gateway lastSeen is
// active and its include filter takes the request, the user's service (serviceName, default
// urac, version 3, api /user/last/seen) and each of its targets is called, without waiting, with
// the request's soajsinjectobj and, when lastSeen has a network, {"network": ...}.
func LastSeen(reg registry.Registry, aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// the request's soajsinjectobj, as set before the service is called
			inject := r.Header.Get("soajsinjectobj")
			next.ServeHTTP(w, r)

			soajsCtx := GetSOAJSContext(r.Context())
			if soajsCtx == nil || soajsCtx.Urac == nil || soajsCtx.Controller == nil || soajsCtx.Controller.ServiceParams == nil {
				return
			}
			regData := reg.Get()
			if regData == nil {
				return
			}
			gateway, _ := regData.CustomRegistry["gateway"].(map[string]interface{})
			value, _ := gateway["value"].(map[string]interface{})
			config, _ := value["lastSeen"].(map[string]interface{})
			if !truthy(config["active"]) {
				return
			}
			info := soajsCtx.Controller.ServiceParams.URLInfo
			var service, api, fullPath string
			if info != nil {
				service, api, fullPath = info.ServiceName, info.ServicePath, info.FullPath
			}
			include, _ := config["include"].(map[string]interface{})
			if _, has := config["include"]; has && truthy(config["include"]) && !lastSeenIncludes(include, service, api, fullPath, r.Method) {
				soajsCtx.Log.Debug("lastSeen skipped for [" + service + " " + r.Method + " " + api + "]")
				return
			}
			profile := soajsCtx.Urac.Profile(false)
			if profile == nil || !truthy(profile["_id"]) {
				soajsCtx.Log.Debug("lastSeen failed with no userId")
				return
			}
			item := lastSeenItem{name: "urac", version: "3", api: "/user/last/seen"}
			if s, ok := config["serviceName"].(string); ok && s != "" {
				item.name = s
			}
			if s, ok := config["serviceVersion"].(string); ok && s != "" {
				item.version = s
			}
			if s, ok := config["api"].(string); ok && s != "" {
				item.api = s
			}
			network := config["network"]
			go notifyLastSeen(regData, aw, item, network, inject)
			if targets, ok := config["targets"].([]interface{}); ok {
				for _, t := range targets {
					target, _ := t.(map[string]interface{})
					name, _ := target["serviceName"].(string)
					path, _ := target["api"].(string)
					if name == "" || path == "" {
						soajsCtx.Log.Debug("lastSeen target skipped, serviceName and api are required")
						continue
					}
					version, _ := target["serviceVersion"].(string)
					method, _ := target["method"].(string)
					go notifyLastSeen(regData, aw, lastSeenItem{name: name, version: version, api: path, method: method}, network, inject)
				}
			}
		})
	}
}

type lastSeenItem struct {
	name, version, api, method string
}

// notifyLastSeen calls one target, without anyone waiting for the answer.
func notifyLastSeen(reg *types.RegistryData, aw awareness.Awareness, item lastSeenItem, network interface{}, inject string) {
	svc := reg.Services[item.name]
	if svc == nil || svc.Port == 0 {
		return
	}
	host, err := aw.GetHost(item.name, item.version)
	if err != nil {
		return
	}
	method := item.method
	if method == "" {
		method = "POST"
	}
	var body io.Reader = http.NoBody
	if truthy(network) {
		raw, _ := json.Marshal(map[string]interface{}{"network": network})
		body = bytes.NewReader(raw)
	}
	ctx, cancel := context.WithTimeout(context.Background(), lastSeenTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), "http://"+net.JoinHostPort(host.Host, strconv.Itoa(svc.Port))+item.api, body)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	if inject != "" {
		req.Header.Set("soajsinjectobj", inject)
	}
	resp, err := lastSeenClient.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
}

// lastSeenIncludes is shouldTriggerLastSeen: the service set to true, or its apis (the path
// relative to the service, else the full path; "*", an exact route, or a route with parameters)
// with true or a list of methods.
func lastSeenIncludes(include map[string]interface{}, service, api, fullPath, method string) bool {
	config, ok := include[service]
	if !ok || !truthy(config) {
		return false
	}
	if config == true {
		return true
	}
	c, ok := config.(map[string]interface{})
	if !ok {
		return false
	}
	apis, ok := c["apis"].(map[string]interface{})
	if !ok || !truthy(c["apis"]) {
		return false
	}
	if api != "" && lastSeenAPIMatch(apis, api, method) {
		return true
	}
	return fullPath != "" && fullPath != api && lastSeenAPIMatch(apis, fullPath, method)
}

func lastSeenAPIMatch(apis map[string]interface{}, path, method string) bool {
	if v, ok := apis["*"]; ok && truthy(v) {
		return lastSeenMethodMatch(v, method)
	}
	if v, ok := apis[path]; ok && truthy(v) {
		return lastSeenMethodMatch(v, method)
	}
	for _, route := range sortedMapKeys(apis) {
		if !strings.Contains(route, "/:") {
			continue
		}
		if re, err := lib.PathToRegexp(route); err == nil && re.MatchString(path) {
			return lastSeenMethodMatch(apis[route], method)
		}
	}
	return false
}

func lastSeenMethodMatch(v interface{}, method string) bool {
	if v == true {
		return true
	}
	if list, ok := v.([]interface{}); ok {
		for _, m := range list {
			if s, ok := m.(string); ok && strings.EqualFold(s, method) {
				return true
			}
		}
	}
	return false
}
