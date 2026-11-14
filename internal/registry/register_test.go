package registry

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

func registryWith(data *types.RegistryData) *MongoRegistry {
	r := NewMongoRegistry(nil, nil, zap.NewNop())
	r.data = data
	return r
}

func TestRegister(t *testing.T) {
	commands := []types.Command{}
	before := &types.RegistryData{
		TimeLoaded: 1,
		Services: map[string]*types.ServiceConfig{"other": {Group: "G", Port: 4200,
			Versions: map[string]*types.VersionInfo{"1": {ExtKeyRequired: true, OAuth: true, Urac: true,
				Maintenance: &types.MaintenanceInfo{Readiness: "/heartbeat", Port: types.PortInfo{Type: "maintenance"}, Commands: &commands}}},
			Hosts: &types.ServiceHosts{Latest: "1", ByVersion: map[string][]string{"1": {"127.0.0.1"}}}}},
		Daemons: map[string]*types.DaemonConfig{},
	}
	r := registryWith(before)
	entry, err := r.Register(RegisterOptions{Name: "other", Group: "G", IP: "127.0.0.3", Type: "service", Version: "1", Port: 4300, Flags: map[string]bool{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(entry)
	// the version keeps its maintenance; the flags are what the registration gave: extKeyRequired false
	want := `"versions":{"1":{"extKeyRequired":false,"maintenance":{"readiness":"/heartbeat","port":{"type":"maintenance"},"commands":[]}}}`
	if !strings.Contains(string(raw), want) {
		t.Errorf("%s", raw)
	}
	if !strings.Contains(string(raw), `"hosts":{"1":["127.0.0.1","127.0.0.3"],"latest":"1"}`) || !strings.Contains(string(raw), `"port":4300`) ||
		!strings.Contains(string(raw), `"version":"1","extKeyRequired":false`) {
		t.Errorf("hosts, port, root: %s", raw)
	}
	// copy on write: the registry a reader held is unchanged
	if !before.Services["other"].Versions["1"].ExtKeyRequired || len(before.Services["other"].Hosts.ByVersion["1"]) != 1 {
		t.Error("the previous registry was modified")
	}
	if r.Get().TimeLoaded == 1 {
		t.Error("a registration marks the registry as loaded again")
	}
	// the logic reads an omitted flag as false
	if r.Get().Services["other"].Versions["1"].OAuth {
		t.Error("an omitted flag reads as false")
	}

	// a new service, a daemon, refusals
	if _, err := r.Register(RegisterOptions{Name: "new", IP: "1.1.1.1", Type: "service", Version: "2", Port: 4400, Flags: map[string]bool{"oauth": true}}); err != nil {
		t.Fatal(err)
	}
	if v := r.Get().Services["new"].Versions["2"]; !v.OAuth || r.Get().Services["new"].Hosts.Latest != "2" {
		t.Errorf("new service %+v", v)
	}
	if _, err := r.Register(RegisterOptions{Name: "worker", IP: "1.1.1.1", Type: "mdaemon", Version: "1", Port: 4500}); err != nil || r.Get().Daemons["worker"] == nil {
		t.Errorf("daemon: %v", err)
	}
	if _, err := r.Register(RegisterOptions{Name: "x", Type: "service", Version: "1"}); err == nil {
		t.Error("an ip is required")
	}
	if _, err := r.Register(RegisterOptions{Name: "nope", IP: "1.1.1.1", Type: "service", Version: "1"}); err == nil {
		t.Error("a new service needs a port")
	}
}

func TestVersionInfoJSON(t *testing.T) {
	full, _ := json.Marshal(types.VersionInfo{ExtKeyRequired: true})
	if string(full) != `{"extKeyRequired":true,"urac":false,"urac_Profile":false,"urac_ACL":false,"urac_Config":false,"urac_GroupConfig":false,"tenant_Profile":false,"provision_ACL":false,"oauth":false,"interConnect":null,"maintenance":null}` {
		t.Errorf("%s", full)
	}
	some, _ := json.Marshal(types.VersionInfo{OAuth: true, Present: map[string]bool{"oauth": true}})
	if string(some) != `{"oauth":true}` {
		t.Errorf("%s", some)
	}
}

func TestParseRegisterFlag(t *testing.T) {
	for v, want := range map[interface{}]bool{true: true, false: false, "true": true, "false": false} {
		if got, ok := ParseRegisterFlag(v); !ok || got != want {
			t.Errorf("%v", v)
		}
	}
	for _, v := range []interface{}{"yes", 1.0, nil} {
		if _, ok := ParseRegisterFlag(v); ok {
			t.Errorf("%v is not a flag", v)
		}
	}
}
