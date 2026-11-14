package urac

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

const defaultResolverTimeout = 2000 * time.Millisecond

// Resolver decides who can access a tenant (mw/mt/tenantAccess.js): the tenant profile
// access.resolver names it, the custom registry urac value.tenantAccessResolvers defines what to
// call.
type Resolver struct {
	Name    string
	Subject interface{}
	Service string
	Version string
	Method  string
	API     string
	Timeout time.Duration
}

// Access is what a resolver grants: the user's groups and allowed packages under the tenant.
type Access struct {
	Groups       []interface{}
	GroupsConfig map[string]interface{}
}

// GetResolver returns the tenant's resolver, nil when the tenant names none or the custom
// registry does not define it.
func GetResolver(reg *types.RegistryData, tenant *types.Tenant, logger *zap.Logger) *Resolver {
	if tenant == nil {
		return nil
	}
	access, _ := tenant.Profile["access"].(map[string]interface{})
	name, _ := access["resolver"].(string)
	if name == "" {
		return nil
	}
	resolvers, _ := uracValue(reg)["tenantAccessResolvers"].(map[string]interface{})
	def, _ := resolvers[name].(map[string]interface{})
	service, _ := def["service"].(string)
	api, _ := def["api"].(string)
	if service == "" || api == "" {
		logger.Error("tenant access resolver is not defined in the urac custom registry",
			zap.String("resolver", name), zap.String("tenant", tenant.Code))
		return nil
	}
	r := &Resolver{Name: name, Subject: access["subject"], Service: service, API: api, Method: "post", Timeout: defaultResolverTimeout}
	if !truthy(r.Subject) {
		r.Subject = nil
	}
	r.Version, _ = def["version"].(string)
	if m, _ := def["method"].(string); m != "" {
		r.Method = m
	}
	if t, ok := def["timeout"].(float64); ok && t != 0 {
		r.Timeout = time.Duration(t * float64(time.Millisecond))
	}
	return r
}

// ResolveRequest is what a resolver call needs from the request.
type ResolveRequest struct {
	Registry *types.RegistryData
	Tenant   *types.Tenant
	// Host returns the host of a service version, "" when there is none.
	Host       func(service, version string) (string, error)
	InjectObj  string
	HTTPClient *http.Client
	Logger     *zap.Logger
}

// Resolve asks the resolver whether the token's user can access the request's tenant, and with
// what groups. Anything but a well formed yes is no access (nil): the caller then reads the user
// record, which catches an invitation made after the token was issued.
func (r *Resolver) Resolve(ctx context.Context, req *ResolveRequest, user map[string]interface{}) *Access {
	username, _ := user["username"].(string)
	logger := req.Logger.With(zap.String("resolver", r.Name), zap.String("username", username), zap.String("tenant", req.Tenant.Code))

	svc := req.Registry.Services[r.Service]
	if svc == nil || svc.Port == 0 {
		logger.Error("tenant access resolver failed, the service is not in the registry", zap.String("service", r.Service))
		return nil
	}
	host, err := req.Host(r.Service, r.Version)
	if err != nil || host == "" {
		logger.Error("tenant access resolver failed, no host", zap.String("service", r.Service), zap.String("version", r.Version))
		return nil
	}
	id, _ := user["id"].(string)
	if id == "" {
		id, _ = user["_id"].(string)
	}
	userTenant, _ := user["tenant"].(map[string]interface{})
	tenant := map[string]interface{}{"id": req.Tenant.ID, "code": req.Tenant.Code}
	if req.Tenant.Main != nil {
		tenant["main"] = req.Tenant.Main
	}
	body, err := json.Marshal(map[string]interface{}{
		"user": map[string]interface{}{
			"id":       nullIfEmpty(id),
			"username": user["username"],
			"tenant":   map[string]interface{}{"id": userTenant["id"], "code": userTenant["code"]},
		},
		"tenant":  tenant,
		"subject": r.Subject,
	})
	if err != nil {
		return nil
	}
	uri := fmt.Sprintf("http://%s:%d%s", host, svc.Port, r.API)
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, strings.ToUpper(r.Method), uri, bytes.NewReader(body))
	if err != nil {
		logger.Error("tenant access resolver failed", zap.Error(err))
		return nil
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("soajsinjectobj", req.InjectObj)
	client := req.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		logger.Error("tenant access resolver failed", zap.Error(err))
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error("tenant access resolver failed", zap.Int("status", resp.StatusCode))
		return nil
	}
	var answer struct {
		Result interface{} `json:"result"`
		Data   interface{} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil || answer.Result != true {
		logger.Error("tenant access resolver returned an invalid response")
		return nil
	}
	data, ok := answer.Data.(map[string]interface{})
	if !ok {
		logger.Error("tenant access resolver returned an invalid response")
		return nil
	}
	if data["access"] != true {
		return nil
	}
	groupsConfig, _ := data["groupsConfig"].(map[string]interface{})
	if !isObject(groupsConfig["allowedPackages"]) {
		logger.Error("tenant access resolver granted access without groupsConfig.allowedPackages")
		return nil
	}
	groups, ok := data["groups"].([]interface{})
	if !ok {
		groups = []interface{}{}
	}
	return &Access{Groups: groups, GroupsConfig: groupsConfig}
}

// isObject is typeof v === "object" for a truthy value: an object or an array.
func isObject(v interface{}) bool {
	switch v.(type) {
	case map[string]interface{}, []interface{}:
		return true
	}
	return false
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// InjectObj is the soajsinjectobj header of a resolver call: the request's tenant, key and
// application as the gateway gives them, without ACL or user. The key config carries only
// commonFields and the resolver service's entry.
func InjectObj(tenant *types.Tenant, iKey, eKey, product, pkg, appID string, servicesConfig map[string]interface{}, service string) string {
	var b bytes.Buffer
	b.WriteString(`{"tenant":`)
	t := []kv{{"id", tenant.ID}, {"name", tenant.Name}, {"code", tenant.Code}, {"locked", tenant.Locked}, {"type", tenant.Type}}
	if tenant.Main != nil {
		t = append(t, kv{"main", tenant.Main})
	}
	writeObject(&b, t)
	b.WriteString(`,"key":{"config":`)
	var config []kv
	if v, ok := servicesConfig["commonFields"]; ok && truthy(v) {
		config = append(config, kv{"commonFields", v})
	}
	if v, ok := servicesConfig[service]; ok && truthy(v) {
		config = append(config, kv{service, v})
	}
	writeObject(&b, config)
	b.WriteString(`,`)
	writeFields(&b, []kv{{"iKey", iKey}, {"eKey", eKey}})
	b.WriteString(`},"application":`)
	writeObject(&b, []kv{{"product", product}, {"package", pkg}, {"appId", appID}})
	b.WriteString(`}`)
	return b.String()
}

type kv struct {
	k string
	v interface{}
}

func writeObject(b *bytes.Buffer, fields []kv) {
	b.WriteString("{")
	writeFields(b, fields)
	b.WriteString("}")
}

func writeFields(b *bytes.Buffer, fields []kv) {
	for i, f := range fields {
		if i > 0 {
			b.WriteString(",")
		}
		k, _ := json.Marshal(f.k)
		v, _ := json.Marshal(f.v)
		b.Write(k)
		b.WriteString(":")
		b.Write(v)
	}
}

// truthy is JavaScript truthiness for JSON values.
func truthy(v interface{}) bool {
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
