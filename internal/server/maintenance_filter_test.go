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
