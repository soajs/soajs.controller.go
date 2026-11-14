package registry

import (
	"encoding/json"
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

func testEnv(t *testing.T) *envDoc {
	t.Helper()
	var e envDoc
	raw := `{"code":"DEV","dbs":{"config":{"prefix":"dev_"},"databases":{
		"oauth":{"cluster":"main","tenantSpecific":false},
		"users":{"cluster":"main","tenantSpecific":false,"prefix":"shared_"},
		"urac":{"cluster":"main","tenantSpecific":true}}},
		"services":{"controller":{"authorization":true,"requestTimeout":0,"requestTimeoutRenewal":2},
		"config":{"ports":{"controller":4000,"maintenanceInc":1000,"randomInc":100}}}}`
	if err := json.Unmarshal([]byte(raw), &e); err != nil {
		t.Fatal(err)
	}
	return &e
}

func TestBuildDatabases(t *testing.T) {
	clusters := map[string]map[string]interface{}{
		"main": {"config": map[string]interface{}{
			"protocol": "mongodb://",
			"servers":  []interface{}{map[string]interface{}{"host": "h", "port": float64(27017)}},
			"URLParam": map[string]interface{}{"maxPoolSize": float64(5)},
		}},
	}
	core, meta, err := buildDatabases(testEnv(t), clusters, "dev", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got := core["oauth"].DatabaseName(); got != "dev_oauth" {
		t.Errorf("oauth database = %s, want dev_oauth", got)
	}
	if got := core["users"].DatabaseName(); got != "shared_users" {
		t.Errorf("a database's own prefix must win: got %s", got)
	}
	if core["oauth"].RegistryLocation.L1 != "coreDB" || core["oauth"].RegistryLocation.Cluster != "main" || core["oauth"].RegistryLocation.TimeLoaded != 7 {
		t.Errorf("unexpected registryLocation %+v", core["oauth"].RegistryLocation)
	}
	if meta["urac"] == nil || meta["urac"].Name != "#TENANT_NAME#_urac" || meta["urac"].RegistryLocation.L1 != "metaDB" {
		t.Errorf("unexpected tenantMetaDB urac %+v", meta["urac"])
	}
	if _, ok := core["urac"]; ok {
		t.Error("a tenant-specific database must not be a core database")
	}

	if _, _, err := buildDatabases(testEnv(t), map[string]map[string]interface{}{}, "dev", 7); err == nil {
		t.Error("a database whose cluster is not a resource must be an error")
	}
}

func TestControllerEntry(t *testing.T) {
	gw := controllerEntry(testEnv(t))
	if gw.Name != "controller" || gw.Group != "Gateway" || gw.Version != "1" || gw.Port != 4000 ||
		gw.RequestTimeout != 30 || gw.RequestTimeoutRenewal != 2 || gw.Authorization == nil || !*gw.Authorization {
		t.Errorf("unexpected controller entry %+v", gw)
	}
}

func TestApplyHosts(t *testing.T) {
	reg := &types.RegistryData{
		Services: map[string]*types.ServiceConfig{
			"controller": {Name: "controller"},
			"orders":     {Port: 4100},
		},
		Daemons: map[string]*types.DaemonConfig{},
	}
	applyHosts(reg, []*types.HostRecord{
		{Env: "dev", Name: "orders", Version: "9", IP: "10.0.0.1", Port: 4101},
		{Env: "dev", Name: "orders", Version: "10", IP: "10.0.0.2", Port: 4102},
		{Env: "dev", Name: "orders", Version: "10", IP: "10.0.0.2", Port: 4102},
		{Env: "dev", Name: "controller", Version: "1", IP: "10.0.0.9"},
		{Env: "dev", Name: "unknown", Version: "1", IP: "10.0.0.3"},
	})
	orders := reg.Services["orders"]
	if orders.Hosts.Latest != "10" {
		t.Errorf("latest = %s, want 10 (versions compare as numbers)", orders.Hosts.Latest)
	}
	if len(orders.Hosts.ByVersion["10"]) != 1 {
		t.Errorf("a host is listed once: %v", orders.Hosts.ByVersion["10"])
	}
	if orders.Port != 4102 || orders.OPort != 4102 {
		t.Errorf("port = %d, oport = %d; the host port replaces the service port", orders.Port, orders.OPort)
	}
	if reg.Services["controller"].Hosts.ByVersion["1"][0] != "10.0.0.9" {
		t.Error("the gateway's hosts go under the controller entry")
	}

	sensitive := &types.RegistryData{Services: map[string]*types.ServiceConfig{"controller": {}, "orders": {Port: 4100}}, Daemons: map[string]*types.DaemonConfig{}}
	applyHosts(sensitive, []*types.HostRecord{{Env: "dashboard", Name: "orders", Version: "1", IP: "10.0.0.1", Port: 5000}})
	if sensitive.Services["orders"].Port != 4100 {
		t.Error("a sensitive environment's host must not replace the service port")
	}
}

func TestSensitiveEnvs(t *testing.T) {
	t.Setenv("SOAJS_SENSITIVE_ENVS", `["Dashboard","portal"]`)
	if !IsSensitiveEnv("portal") || !IsSensitiveEnv("DASHBOARD") || IsSensitiveEnv("dev") {
		t.Errorf("SOAJS_SENSITIVE_ENVS not applied: %v", SensitiveEnvs())
	}
	t.Setenv("SOAJS_SENSITIVE_ENVS", "not json")
	if got := SensitiveEnvs(); len(got) != 1 || got[0] != "dashboard" {
		t.Errorf("default = %v, want [dashboard]", got)
	}
}

func TestAvailable(t *testing.T) {
	if !available(map[string]interface{}{"created": "DEV"}, "DEV") {
		t.Error("created in the environment")
	}
	if !available(map[string]interface{}{"created": "STG", "shared": true, "sharedEnvs": map[string]interface{}{"DEV": true}}, "DEV") {
		t.Error("shared to the environment")
	}
	if available(map[string]interface{}{"created": "STG", "shared": true, "sharedEnvs": map[string]interface{}{"PROD": true}}, "DEV") {
		t.Error("shared to another environment")
	}
}
