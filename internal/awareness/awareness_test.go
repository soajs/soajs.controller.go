package awareness

import (
	"encoding/json"
	"errors"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type testRegistry struct {
	mu       sync.Mutex
	data     *types.RegistryData
	others   map[string]*types.RegistryData
	hosts    []types.HostRecord
	hostsErr error
	docs     []map[string]interface{}
}

func (r *testRegistry) Load(string) error { return nil }
func (r *testRegistry) Reload() error     { return nil }
func (r *testRegistry) Get() *types.RegistryData {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data
}
func (r *testRegistry) Register(registry.RegisterOptions) (interface{}, error) { return nil, nil }

func (r *testRegistry) LoadByEnv(env string, _ bool) (*types.RegistryData, error) {
	if d, ok := r.others[env]; ok {
		return d, nil
	}
	return nil, errors.New("Registry not found for:" + env)
}
func (r *testRegistry) RegisterHost(types.HostRecord) error { return nil }
func (r *testRegistry) GetHost(string, string) (*types.HostInfo, error) {
	return nil, errors.New("unused")
}
func (r *testRegistry) OtherEnvControllerHosts(string) ([]types.HostRecord, error) {
	return r.hosts, r.hostsErr
}
func (r *testRegistry) UpsertEnvController(env, ip string, ts int64, data map[string]interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.docs = append(r.docs, map[string]interface{}{"env": env, "ip": ip, "data": data})
	return nil
}
func (r *testRegistry) GetEnvironment() string { return "dev" }
func (r *testRegistry) Close()                 {}

// server answers 200 on the paths in ok and 404 elsewhere; it returns its host and port.
func server(t *testing.T, ok ...string) (string, int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range ok {
			if r.URL.Path == p {
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	host, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	p, _ := strconv.Atoi(port)
	return host, p
}

func hosts(latest string, byVersion map[string][]string) *types.ServiceHosts {
	return &types.ServiceHosts{Latest: latest, ByVersion: byVersion}
}

func regWith(services map[string]*types.ServiceConfig, inc int) *types.RegistryData {
	return &types.RegistryData{
		TimeLoaded: 1,
		ServiceConfig: &types.GlobalServiceConfig{
			Ports:     types.PortsConfig{MaintenanceInc: inc},
			Awareness: types.AwarenessConfig{MaxLogCount: 5, HealthCheckInterval: 5000},
		},
		Services: services,
	}
}

func TestCustomHealthChecks(t *testing.T) {
	_, readiness := server(t, "/ready")
	_, maint := server(t, "/heartbeat")
	_, svc := server(t, "/heartbeat")
	_, down := server(t)
	reg := &testRegistry{}
	// one maintenance port increment for all: services sit at maint-inc so that port+inc = maint
	inc := 1000
	reg.data = regWith(map[string]*types.ServiceConfig{
		// readiness on the maintenance port
		"a": {Port: readiness - inc, Hosts: hosts("1", map[string][]string{"1": {"127.0.0.1"}}),
			Maintenance: &types.RootMaintenance{Readiness: "/ready", Port: types.PortInfo{Type: "maintenance"}}},
		// readiness on the service port (inherit)
		"b": {Port: readiness, Hosts: hosts("1", map[string][]string{"1": {"127.0.0.1"}}),
			Maintenance: &types.RootMaintenance{Readiness: "/ready", Port: types.PortInfo{Type: "inherit"}}},
		// no maintenance: /heartbeat at the maintenance port
		"c": {Port: maint - inc, Hosts: hosts("1", map[string][]string{"1": {"127.0.0.1"}})},
		// no maintenance: /heartbeat at the service port when the maintenance port fails
		"d": {Port: svc, Hosts: hosts("1", map[string][]string{"1": {"127.0.0.1"}})},
		"e": {Port: down, Hosts: hosts("1", map[string][]string{"1": {"127.0.0.1"}})},
	}, inc)
	c := NewCustom(reg, Gateway{Name: "controller", Version: "1"}, zap.NewNop())
	c.Check()
	for _, name := range []string{"a", "b", "c", "d"} {
		if h, err := c.GetHost(name, ""); err != nil || h.Host != "127.0.0.1" {
			t.Errorf("%s: %v %v", name, h, err)
		}
	}
	if _, err := c.GetHost("e", ""); err != ErrNoHost {
		t.Errorf("an unhealthy host is not returned: %v", err)
	}

	// awarenessStats: a host that goes down, then stays down
	c.now = func() time.Time { return time.UnixMilli(1000) }
	c.Check()
	c.now = func() time.Time { return time.UnixMilli(2000) }
	c.Check()
	s := c.Stats()["services"]["e"]["127.0.0.1"]
	if s.DownCount != 2 || s.LastCheck != 2000 {
		t.Errorf("down three checks: %+v", s)
	}
	raw, _ := json.Marshal(s)
	if !strings.HasPrefix(string(raw), `{"lastCheck":2000,"healthy":false,"version":"1","downCount":2,"downSince":`) {
		t.Errorf("a host still down writes downCount first: %s", raw)
	}
	first := &HostStat{LastCheck: 5, Version: "1", DownSince: 5, downFirst: true}
	if raw, _ := json.Marshal(first); string(raw) != `{"lastCheck":5,"healthy":false,"version":"1","downSince":5,"downCount":0}` {
		t.Errorf("the first down check writes downSince first: %s", raw)
	}
	up := &HostStat{LastCheck: 5, Healthy: true, Version: "1"}
	if raw, _ := json.Marshal(up); string(raw) != `{"lastCheck":5,"healthy":true,"version":"1"}` {
		t.Errorf("%s", raw)
	}

	// a reload of the registry starts the stats over, the healthy hosts stay
	reg.mu.Lock()
	reg.data = regWith(reg.data.Services, inc)
	reg.data.TimeLoaded = 2
	reg.mu.Unlock()
	c.mu.Lock()
	c.rebuild(reg.Get())
	c.mu.Unlock()
	if len(c.Stats()) != 0 {
		t.Error("stats start over")
	}
	if _, err := c.GetHost("a", "1"); err != nil {
		t.Error("healthy hosts stay")
	}
}

func TestCustomRoundRobin(t *testing.T) {
	reg := &testRegistry{data: regWith(map[string]*types.ServiceConfig{
		"s": {Port: 4000, Hosts: hosts("2", map[string][]string{"1": {"h1"}, "2": {"h1", "h2", "h3"}})},
	}, 1000)}
	c := NewCustom(reg, Gateway{Name: "controller"}, zap.NewNop())
	c.healthy["s"] = map[string][]string{"2": {"h1", "h3"}, "1": {}}
	c.indexes["s"] = map[string]int{}
	var got []string
	for i := 0; i < 4; i++ {
		h, err := c.GetHost("s", "")
		if err != nil {
			t.Fatal(err)
		}
		if h.Port != 4000 || h.Version != "2" {
			t.Errorf("%+v", h)
		}
		got = append(got, h.Host)
	}
	if strings.Join(got, ",") != "h1,h3,h1,h3" {
		t.Errorf("round-robin over the healthy hosts: %v", got)
	}
	if _, err := c.GetHost("s", "1"); err != ErrNoHost {
		t.Error("no healthy host of version 1")
	}
	// a healthy host that left the registry: one pass, then no host (Node.js recurses forever)
	c.healthy["s"]["2"] = []string{"gone"}
	if _, err := c.GetHost("s", "2"); err != ErrNoHost {
		t.Error("a stale healthy list answers no host")
	}
	if _, err := c.GetHost("missing", ""); err != ErrNoHost {
		t.Error("unknown service")
	}
	if c.GetLatestVersion("s") != "2" {
		t.Error("latest from the registry's hosts")
	}
}

func k8sService(name, version, typ, ip string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dev-" + name + "-v" + version + "-service",
			Namespace: "soajs",
			Labels:    map[string]string{"soajs.content": "true", "soajs.service.name": name, "soajs.service.version": version, "soajs.service.type": typ},
		},
		Spec: corev1.ServiceSpec{ClusterIP: ip},
	}
}

func haRegistry(namespace string) *types.RegistryData {
	k8s := map[string]interface{}{"id": "6a0e000000000000000000f1", "configuration": map[string]interface{}{"url": "k8s.local", "token": "t", "port": 6443.0}}
	if namespace != "" {
		k8s["namespace"] = namespace
	}
	return &types.RegistryData{
		ServiceConfig: &types.GlobalServiceConfig{Ports: types.PortsConfig{Controller: 4000}},
		Services:      map[string]*types.ServiceConfig{"controller": {Port: 4000}, "urac": {Port: 4001}},
		Deployer:      map[string]interface{}{"type": "container", "selected": "container.kubernetes", "container": map[string]interface{}{"kubernetes": k8s}},
	}
}

func TestHA(t *testing.T) {
	t.Setenv("SOAJS_DEPLOY_HA", "kubernetes")
	cs := fake.NewSimpleClientset(
		k8sService("urac", "3", "service", "10.0.0.3"),
		k8sService("urac", "2", "service", "10.0.0.2"),
		k8sService("worker", "1", "mdaemon", "10.0.1.1"),
		k8sService("db", "1", "other", "10.0.2.1"),
	)
	var connected KubeConfig
	connect := func(cfg KubeConfig) (KubeClient, error) {
		connected = cfg
		return NewKubeClient(cs), nil
	}
	reg := &testRegistry{data: haRegistry("soajs")}
	h := NewHA(reg, Gateway{Name: "controller", Version: "1"}, zap.NewNop(), connect)
	h.Rebuild()
	if connected.URL != "https://k8s.local:6443" || connected.Namespace != "soajs" {
		t.Errorf("connected to %+v", connected)
	}
	if got, _ := h.GetHost("urac", "2"); got == nil || got.Host != "10.0.0.2" || got.Port != 4001 {
		t.Errorf("cached: %+v", got)
	}
	if h.GetLatestVersion("urac") != "3" || h.GetLatestVersion("worker") != "1" || h.GetLatestVersion("db") != "" {
		t.Error("latest from the cache, services and mdaemons only")
	}
	// the gateway's own host, in its namespace
	if got, _ := h.GetHost("controller", ""); got == nil || got.Host != "dev-controller-v1-service.soajs" {
		t.Errorf("gateway: %+v", got)
	}
	// SOAJS_DEPLOY_HA=kubernetes with a version: the service name in the cluster
	if got, _ := h.GetHost("oauth", "4"); got == nil || got.Host != "dev-oauth-v4-service" {
		t.Errorf("by name: %+v", got)
	}
	// no version, not cached: the latest version from the cluster, then its cluster IP
	h.cache = map[string]map[string]string{}
	if got, err := h.GetHost("urac", ""); err != nil || got.Host != "10.0.0.3" || got.Version != "3" {
		t.Errorf("from the API: %+v %v", got, err)
	}
	if v, ok := h.GetLatestVersionFromCluster("urac"); !ok || v != "3" {
		t.Errorf("latest from the cluster: %q", v)
	}

	t.Setenv("SOAJS_DEPLOY_HA", "true")
	if got, _ := h.GetHost("controller", ""); got == nil || got.Host != "dev-controller.soajs" {
		t.Errorf("gateway without the kubernetes suffix: %+v", got)
	}
	// without a namespace there is no gateway host
	reg.data = haRegistry("")
	if _, err := h.GetHost("controller", ""); err != ErrNoHost {
		t.Error("no namespace, no host")
	}
}

func TestHAConfiguration(t *testing.T) {
	conf := func(reg *types.RegistryData) map[string]interface{} {
		return reg.Deployer["container"].(map[string]interface{})["kubernetes"].(map[string]interface{})["configuration"].(map[string]interface{})
	}

	// without a ca: url, port and protocol, the server certificate not verified (Node.js)
	reg := haRegistry("soajs")
	cfg, err := kubeConfig(reg)
	if err != nil || cfg.URL != "https://k8s.local:6443" || !cfg.Insecure || cfg.CAData != nil || cfg.Token != "t" {
		t.Errorf("%+v %v", cfg, err)
	}
	rc := restConfig(cfg)
	if !rc.TLSClientConfig.Insecure || rc.BearerToken != "t" {
		t.Errorf("rest config %+v", rc.TLSClientConfig)
	}
	conf(reg)["protocol"] = "http"
	delete(conf(reg), "port")
	if cfg, err := kubeConfig(reg); err != nil || cfg.URL != "http://k8s.local" {
		t.Errorf("%+v %v", cfg, err)
	}

	// with a ca: its request options, verified against its CA
	reg = haRegistry("soajs")
	conf(reg)["ca"] = map[string]interface{}{"requestOptions": map[string]interface{}{
		"baseUrl": "https://api.cluster:443", "ca": "-----BEGIN CERTIFICATE-----\nX\n-----END CERTIFICATE-----",
		"cert": "CERT", "key": "KEY",
	}}
	cfg, err = kubeConfig(reg)
	if err != nil || cfg.URL != "https://api.cluster:443" || cfg.Insecure || string(cfg.CAData) == "" || cfg.Token != "t" ||
		string(cfg.CertData) != "CERT" || string(cfg.KeyData) != "KEY" {
		t.Errorf("%+v %v", cfg, err)
	}
	rc = restConfig(cfg)
	if rc.TLSClientConfig.Insecure || string(rc.TLSClientConfig.CAData) != string(cfg.CAData) || rc.Host != "https://api.cluster:443" {
		t.Errorf("rest config %+v", rc.TLSClientConfig)
	}
	// a list of CAs, its own token
	conf(reg)["ca"].(map[string]interface{})["requestOptions"].(map[string]interface{})["ca"] = []interface{}{"A", "B"}
	conf(reg)["ca"].(map[string]interface{})["requestOptions"].(map[string]interface{})["auth"] = map[string]interface{}{"bearer": "own"}
	if cfg, err := kubeConfig(reg); err != nil || string(cfg.CAData) != "A\nB" || cfg.Token != "own" {
		t.Errorf("%+v %v", cfg, err)
	}
	// strictSSL false: not verified, no CA passed (client-go refuses both)
	conf(reg)["ca"].(map[string]interface{})["requestOptions"].(map[string]interface{})["strictSSL"] = false
	if cfg, err := kubeConfig(reg); err != nil || !cfg.Insecure || cfg.CAData != nil {
		t.Errorf("%+v %v", cfg, err)
	}

	if _, err := kubeConfig(&types.RegistryData{Deployer: map[string]interface{}{"type": "manual", "selected": "manual"}}); err == nil {
		t.Error("a manual deployer has no cluster")
	}
}

func TestEnvCustom(t *testing.T) {
	host, maint := server(t, "/heartbeat")
	_, down := server(t)
	reg := &testRegistry{data: regWith(map[string]*types.ServiceConfig{"controller": {Port: maint - 1000}}, 1000)}
	reg.hosts = []types.HostRecord{
		{Env: "stg", Name: "controller", Version: "1", IP: host},
		{Env: "stg", Name: "controller", Version: "2", IP: host},
		{Env: "prod", Name: "controller", Version: "1", IP: "127.0.0.2"},
	}
	e := NewEnvCustom(reg, Gateway{Name: "controller", Version: "1"}, zap.NewNop())
	e.Check()
	if got := e.GetHost("stg", ""); got != host {
		t.Errorf("latest version of stg: %q", got)
	}
	if got := e.GetHost("STG", "1"); got != host {
		t.Errorf("version 1: %q", got)
	}
	if got := e.GetHost("prod", ""); got != "" {
		t.Errorf("an unhealthy controller: %q", got)
	}
	if got := e.GetHost("qa", ""); got != "" {
		t.Errorf("an environment without hosts: %q", got)
	}

	// the heartbeat is on this gateway's maintenance port: a gateway whose port does not answer
	reg.data = regWith(map[string]*types.ServiceConfig{"controller": {Port: down - 1000}}, 1000)
	reg.data.TimeLoaded = 2
	e.Check()
	if got := e.GetHost("stg", "1"); got != "" {
		t.Errorf("an unhealthy host leaves the list: %q", got)
	}

	// a failed reload keeps the previous hosts
	reg.hostsErr = errors.New("db down")
	e.Fetch()
	if len(e.hosts) != 3 {
		t.Error("previous hosts kept")
	}
}

func TestEnvHA(t *testing.T) {
	t.Setenv("SOAJS_DEPLOY_HA", "kubernetes")
	reg := &testRegistry{data: haRegistry("dev-ns"), others: map[string]*types.RegistryData{
		"stg":  haRegistry("stg-ns"),
		"bare": haRegistry(""),
	}}
	e := NewEnvHA(reg, Gateway{Name: "controller", Version: "1"})
	if got := e.GetHost("STG", "5"); got != "stg-controller-v1-service.stg-ns" {
		t.Errorf("the target environment's namespace, the gateway's version: %q", got)
	}
	if got := e.GetHost("", ""); got != "dev-controller-v1-service.dev-ns" {
		t.Errorf("own environment: %q", got)
	}
	// no registry for the environment, or no namespace: no host, never a guessed one
	if got := e.GetHost("qa", ""); got != "" {
		t.Errorf("no registry: %q", got)
	}
	if got := e.GetHost("bare", ""); got != "" {
		t.Errorf("no namespace: %q", got)
	}
}

func TestControllerData(t *testing.T) {
	reg := &types.RegistryData{
		Services: map[string]*types.ServiceConfig{
			"controller": {Port: 4000, Hosts: hosts("1", map[string][]string{"1": {"10.0.0.1"}})},
			"urac":       {Port: 4001, Hosts: hosts("1.2", map[string][]string{"1.2": {"10.0.0.2"}})},
		},
		Daemons: map[string]*types.DaemonConfig{"worker": {Port: 4100}},
	}
	stats := map[string]map[string]map[string]*HostStat{"services": {
		"controller": {"10.0.0.1": {LastCheck: 1, Healthy: true, Version: "1"}},
		"urac":       {"10.0.0.2": {LastCheck: 1, Healthy: true, Version: "1.2"}},
	}}
	data := ControllerData(reg, stats, "controller", "10.0.0.1", false, time.UnixMilli(9))
	urac := data["services"].(map[string]interface{})["urac"].(map[string]interface{})
	if _, ok := urac["awarenessStats"].(map[string]interface{})["10_dot_0_dot_0_dot_2"]; !ok {
		t.Errorf("IP keys: %v", urac["awarenessStats"])
	}
	if _, ok := urac["hosts"].(map[string]interface{})["1x2"]; !ok {
		t.Errorf("version keys: %v", urac["hosts"])
	}
	if data["daemons"].(map[string]interface{})["worker"] == nil {
		t.Error("daemons")
	}

	data = ControllerData(reg, stats, "controller", "10.0.0.1", true, time.UnixMilli(9))
	services := data["services"].(map[string]interface{})
	if len(services) != 1 || len(data["daemons"].(map[string]interface{})) != 0 {
		t.Errorf("terminating keeps the gateway only, daemons empty: %v", data)
	}
	stat := services["controller"].(map[string]interface{})["awarenessStats"].(map[string]interface{})["10_dot_0_dot_0_dot_1"].(map[string]interface{})
	if stat["healthy"] != false || stat["lastCheck"] != int64(9) {
		t.Errorf("this gateway is marked down: %v", stat)
	}

	t.Setenv("SOAJS_MANUAL", "")
	if ControllerIP("10.0.0.1") != "127.0.0.1" {
		t.Error("without SOAJS_MANUAL the document is kept under 127.0.0.1")
	}
	t.Setenv("SOAJS_MANUAL", "1")
	if ControllerIP("10.0.0.1") != "10.0.0.1" {
		t.Error("with SOAJS_MANUAL, the gateway's IP")
	}
}

func TestServiceIP(t *testing.T) {
	t.Setenv("SOAJS_DEPLOY_HA", "")
	t.Setenv("SOAJS_SRVIP", "10.1.1.1")
	if ServiceIP() != "10.1.1.1" {
		t.Error("SOAJS_SRVIP")
	}
	t.Setenv("SOAJS_SRV_AUTOREGISTERHOST", "false")
	if ServiceIP() != "127.0.0.1" {
		t.Error("auto registration off")
	}
	t.Setenv("SOAJS_SRV_AUTOREGISTERHOST", "true")
	t.Setenv("SOAJS_SRVIP", "")
	if ServiceIP() == "" {
		t.Error("detected or 127.0.0.1")
	}
	t.Setenv("SOAJS_DEPLOY_HA", "kubernetes")
	if ServiceIP() != "" {
		t.Error("no registration in HA mode")
	}
}

func TestRegisterAndWriteDoc(t *testing.T) {
	t.Setenv("SOAJS_DEPLOY_HA", "")
	reg := &testRegistry{data: regWith(map[string]*types.ServiceConfig{"controller": {Port: 4000}}, 1000)}
	reg.data.ServiceConfig.Awareness.HealthCheckInterval = 1
	c := NewCustom(reg, Gateway{Name: "controller", Version: "1"}, zap.NewNop())
	if !Register(reg, c, Gateway{Name: "controller", Version: "1"}, 4000, "10.0.0.1", zap.NewNop()) {
		t.Fatal("registered")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		reg.mu.Lock()
		n := len(reg.docs)
		reg.mu.Unlock()
		if n > 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.docs) != 1 || reg.docs[0]["env"] != "dev" {
		t.Errorf("controllers document after healthCheckInterval: %v", reg.docs)
	}
	t.Setenv("SOAJS_DEPLOY_HA", "true")
	if Register(reg, c, Gateway{Name: "controller"}, 4000, "10.0.0.1", zap.NewNop()) {
		t.Error("no registration in HA mode")
	}
}

// configuration.ca is optional; when present it holds the client's request options.
func TestInfraSchemaCA(t *testing.T) {
	schemas, err := dataschema.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	infra := schemas["infra"]
	doc := func(ca string) []byte {
		c := ""
		if ca != "" {
			c = `,"ca":` + ca
		}
		return []byte(`{"_id":{"$oid":"6a0e000000000000000000f1"},"type":"kubernetes","configuration":{"url":"k8s.local","token":"t"` + c + `}}`)
	}
	for name, c := range map[string]struct {
		ca    string
		valid bool
	}{
		"no ca":           {"", true},
		"request options": {`{"requestOptions":{"baseUrl":"https://k8s.local","ca":"PEM","strictSSL":true}}`, true},
		"a list of CAs":   {`{"requestOptions":{"baseUrl":"https://k8s.local","ca":["A","B"]}}`, true},
		"no baseUrl":      {`{"requestOptions":{"ca":"PEM"}}`, false},
		"auth provider":   {`{"requestOptions":{"baseUrl":"https://k8s.local"},"authProvider":{"type":"cmd"}}`, false},
		"a PEM string":    {`"PEM"`, false},
	} {
		problems, err := infra.ValidateJSON(doc(c.ca))
		if err != nil {
			t.Fatal(err)
		}
		if (problems == nil) != c.valid {
			t.Errorf("%s: valid %v, problems %v", name, c.valid, problems)
		}
	}
}

// The calls the HA awareness makes to the API server: services in the gateway's namespace, by
// label, and one service by name. They were the same on client-go v0.29.3.
func TestHAAPICalls(t *testing.T) {
	t.Setenv("SOAJS_DEPLOY_HA", "kubernetes")
	cs := fake.NewSimpleClientset(k8sService("urac", "3", "service", "10.0.0.3"))
	connect := func(cfg KubeConfig) (KubeClient, error) { return NewKubeClient(cs), nil }
	h := NewHA(&testRegistry{data: haRegistry("soajs")}, Gateway{Name: "controller", Version: "1"}, zap.NewNop(), connect)
	h.Rebuild()
	h.cache = map[string]map[string]string{}
	h.GetHost("urac", "")
	h.GetLatestVersionFromCluster("urac")
	var got []string
	for _, a := range cs.Actions() {
		s := a.GetVerb() + " " + a.GetResource().Resource + " " + a.GetNamespace()
		if l, ok := a.(k8stesting.ListAction); ok {
			s += " " + l.GetListRestrictions().Labels.String()
		}
		if g, ok := a.(k8stesting.GetAction); ok {
			s += " " + g.GetName()
		}
		got = append(got, s)
	}
	want := []string{
		"list services soajs soajs.content=true",
		"list services soajs soajs.service.name=urac",
		"get services soajs dev-urac-v3-service",
		"list services soajs soajs.service.name=urac",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("API calls:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
