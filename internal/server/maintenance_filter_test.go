package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/dbconfig"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

func filterTestRegistry() *types.RegistryData {
	auth := true
	return &types.RegistryData{
		Name:        "dev",
		Environment: "dev",
		CoreDB:      map[string]*dbconfig.DB{"provision": {Name: "core_provision"}},
		ServiceConfig: &types.GlobalServiceConfig{
			CORS:  types.CORSConfig{Enabled: true, Origin: "*"},
			OAuth: types.OAuthConfig{Grants: []string{"password"}},
			Key:   types.KeyEncryptionConfig{Algorithm: "aes256", Password: "k"},
		},
		Deployer: map[string]interface{}{
			"type":     "container",
			"selected": "container.kubernetes",
			"container": map[string]interface{}{"kubernetes": map[string]interface{}{
				"id": "x", "namespace": "dev",
				"configuration": map[string]interface{}{"url": "api.example.com", "token": "secret-token"},
			}},
		},
		Services: map[string]*types.ServiceConfig{
			"controller": {Name: "controller", Authorization: &auth},
			"orders":     {Group: "Commerce", Port: 4100, RequestTimeout: 30},
			"payments":   {Group: "Commerce", Port: 4200},
		},
		Daemons: map[string]*types.DaemonConfig{},
	}
}

// The registry services receive never holds the kubernetes token, cors, or oauth (except for oauth).
func TestCloneAndFilterRegistry(t *testing.T) {
	reg := filterTestRegistry()
	raw, _ := json.Marshal(cloneAndFilterRegistry(reg, "orders", "service"))
	body := string(raw)
	if strings.Contains(body, "secret-token") {
		t.Error("the deployer token reached a service")
	}
	if strings.Contains(body, `"cors"`) || strings.Contains(body, `"oauth"`) {
		t.Error("cors or oauth reached a service other than oauth")
	}
	if !strings.Contains(body, `"orders":{"group":"Commerce","port":4100}`) || strings.Contains(body, `"payments"`) {
		t.Errorf("a service sees only the gateway and itself: %s", body)
	}
	if !strings.Contains(body, `"url":"api.example.com"`) {
		t.Error("the rest of the deployer configuration must stay")
	}
	if reg.Deployer["container"].(map[string]interface{})["kubernetes"].(map[string]interface{})["configuration"].(map[string]interface{})["token"] != "secret-token" {
		t.Error("filtering must not change the registry in use")
	}

	raw, _ = json.Marshal(cloneAndFilterRegistry(reg, "oauth", "service"))
	if !strings.Contains(string(raw), `"oauth":{`) {
		t.Error("the oauth service receives serviceConfig.oauth")
	}

	raw, _ = json.Marshal(cloneAndFilterRegistry(reg, "", ""))
	if body := string(raw); !strings.Contains(body, `"payments"`) || strings.Contains(body, "secret-token") || strings.Contains(body, `"cors"`) {
		t.Errorf("/reloadRegistry: every service, no token, no cors: %s", body)
	}
}

// Databases are given as the Node.js registry gives them: the configuration as stored, with the
// options its connections consumed left out until the registry is loaded again; coreDB.session.
func TestDatabaseDumps(t *testing.T) {
	reg := filterTestRegistry()
	reg.CoreDB["oauth"] = &dbconfig.DB{Dump: map[string]interface{}{
		"prefix": "p_", "cluster": "c", "URLParam": map[string]interface{}{"maxPoolSize": 5.0, "authSource": "admin"}, "streaming": map[string]interface{}{}, "name": "oauth",
	}}
	reg.CoreDB["provision"] = &dbconfig.DB{Dump: map[string]interface{}{"name": "core_provision", "extraParam": map[string]interface{}{}, "credentials": nil}}
	reg.SessionDB = map[string]interface{}{"prefix": "p_", "servers": nil}
	raw, _ := json.Marshal(cloneAndFilterRegistry(reg, "", ""))
	body := string(raw)
	if strings.Contains(body, "maxPoolSize") || strings.Contains(body, "extraParam") || !strings.Contains(body, `"authSource":"admin"`) ||
		!strings.Contains(body, `"streaming":{}`) || !strings.Contains(body, `"credentials":null`) || !strings.Contains(body, `"session":{"prefix":"p_","servers":null}`) {
		t.Errorf("first load: %s", body)
	}
	reg.Reloaded = true
	raw, _ = json.Marshal(cloneAndFilterRegistry(reg, "", ""))
	if !strings.Contains(string(raw), `"maxPoolSize":5`) || !strings.Contains(string(raw), `"extraParam":{}`) {
		t.Errorf("after a reload: %s", raw)
	}
	if reg.CoreDB["oauth"].Dump["URLParam"].(map[string]interface{})["maxPoolSize"] != 5.0 {
		t.Error("the registry in use is not changed")
	}
}

func TestValidVersion(t *testing.T) {
	for v, want := range map[string]bool{"1": true, "1.2": true, "v1": true, "": false, "abc": false} {
		if validVersion(v) != want {
			t.Errorf("%q", v)
		}
	}
}
