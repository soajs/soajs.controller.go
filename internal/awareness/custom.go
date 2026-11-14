package awareness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// checkTimeout bounds one heartbeat, as Node.js checkStatus (lib/request.js).
const checkTimeout = 5 * time.Second

// HostStat is the health of a host (awarenessStats).
type HostStat struct {
	LastCheck int64
	Healthy   bool
	Version   string
	DownCount int
	DownSince int64
	// downFirst: Node.js writes downSince before downCount for a host that just went down
	downFirst bool
}

// MarshalJSON writes the fields in the order Node.js sets them.
func (s *HostStat) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	fmt.Fprintf(&b, `{"lastCheck":%d,"healthy":%t,"version":%s`, s.LastCheck, s.Healthy, quote(s.Version))
	if !s.Healthy {
		if s.downFirst {
			fmt.Fprintf(&b, `,"downSince":%d,"downCount":%d`, s.DownSince, s.DownCount)
		} else {
			fmt.Fprintf(&b, `,"downCount":%d,"downSince":%d`, s.DownCount, s.DownSince)
		}
	}
	b.WriteString("}")
	return b.Bytes(), nil
}

func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

type target struct {
	name, what, version, host string
	port                      int
}

// Custom is the awareness without SOAJS_DEPLOY_HA (mw/awareness/custom.js): every
// healthCheckInterval, each host of each service and daemon is checked on its maintenance
// readiness, else on /heartbeat at the maintenance port then the service port. GetHost takes the
// healthy hosts in turn.
type Custom struct {
	reg    registry.Registry
	gw     Gateway
	logger *zap.Logger
	client *http.Client

	mu         sync.Mutex
	loadedTime int64
	targets    []target
	healthy    map[string]map[string][]string
	indexes    map[string]map[string]int
	stats      map[string]map[string]map[string]*HostStat
	now        func() time.Time
}

// NewCustom returns the registry-hosts awareness; Start begins the health checks.
func NewCustom(reg registry.Registry, gw Gateway, logger *zap.Logger) *Custom {
	return &Custom{
		reg:     reg,
		gw:      gw,
		logger:  logger,
		client:  &http.Client{Timeout: checkTimeout},
		healthy: map[string]map[string][]string{},
		indexes: map[string]map[string]int{},
		stats:   map[string]map[string]map[string]*HostStat{},
		now:     time.Now,
	}
}

// Start runs the health checks every services.config.awareness.healthCheckInterval ms, when set,
// until ctx is done.
func (c *Custom) Start(ctx context.Context) {
	reg := c.reg.Get()
	if reg == nil || reg.ServiceConfig == nil || reg.ServiceConfig.Awareness.HealthCheckInterval <= 0 {
		return
	}
	go func() {
		for {
			go c.Check()
			interval := time.Duration(reg.ServiceConfig.Awareness.HealthCheckInterval) * time.Millisecond
			if r := c.reg.Get(); r != nil && r.ServiceConfig != nil && r.ServiceConfig.Awareness.HealthCheckInterval > 0 {
				interval = time.Duration(r.ServiceConfig.Awareness.HealthCheckInterval) * time.Millisecond
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(interval):
			}
		}
	}()
}

// Check runs one round of health checks and returns when every host was checked.
func (c *Custom) Check() {
	reg := c.reg.Get()
	if reg == nil || reg.ServiceConfig == nil {
		return
	}
	c.mu.Lock()
	if c.loadedTime != reg.TimeLoaded {
		c.rebuild(reg)
	}
	targets := c.targets
	c.mu.Unlock()

	var wg sync.WaitGroup
	for _, t := range targets {
		wg.Add(1)
		go func(t target) {
			defer wg.Done()
			c.record(reg, t, c.checkTarget(reg, t))
		}(t)
	}
	wg.Wait()
}

// rebuild lists the hosts to check after the registry was loaded. Healthy hosts are kept, the
// stats start over as they live on the registry in Node.js.
func (c *Custom) rebuild(reg *types.RegistryData) {
	c.loadedTime = reg.TimeLoaded
	c.targets = nil
	c.stats = map[string]map[string]map[string]*HostStat{}
	add := func(what, name string, hosts *types.ServiceHosts, port int) {
		if c.healthy[name] == nil {
			c.healthy[name] = map[string][]string{}
			c.indexes[name] = map[string]int{}
		}
		if hosts == nil {
			return
		}
		for _, v := range sortedKeys(hosts.ByVersion) {
			if c.healthy[name][v] == nil {
				c.healthy[name][v] = []string{}
			}
			for _, h := range hosts.ByVersion[v] {
				c.targets = append(c.targets, target{name: name, what: what, version: v, host: h, port: port})
			}
		}
	}
	for _, name := range sortedKeys(reg.Services) {
		s := reg.Services[name]
		add("services", name, s.Hosts, s.Port)
	}
	for _, name := range sortedKeys(reg.Daemons) {
		d := reg.Daemons[name]
		add("daemons", name, d.Hosts, d.Port)
	}
}

// checkTarget checks a host: on the service's maintenance readiness when the service declares
// one (Node.js looks it up under services, for daemons too), else on /heartbeat at the
// maintenance port, then at the service port.
func (c *Custom) checkTarget(reg *types.RegistryData, t target) bool {
	inc := reg.ServiceConfig.Ports.MaintenanceInc
	if svc := reg.Services[t.name]; svc != nil && svc.Maintenance != nil && svc.Maintenance.Readiness != "" && svc.Maintenance.Port.Type != "" {
		port := t.port
		if svc.Maintenance.Port.Type == "maintenance" {
			port += inc
		}
		return c.heartbeat(t.host, port, svc.Maintenance.Readiness)
	}
	if c.heartbeat(t.host, t.port+inc, "/heartbeat") {
		return true
	}
	return c.heartbeat(t.host, t.port, "/heartbeat")
}

// heartbeat is Node.js checkStatus: healthy on a 200 within 5 seconds.
func (c *Custom) heartbeat(host string, port int, path string) bool {
	resp, err := c.client.Get(fmt.Sprintf("http://%s:%d%s", host, port, path))
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func (c *Custom) record(reg *types.RegistryData, t target, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stats[t.what] == nil {
		c.stats[t.what] = map[string]map[string]*HostStat{}
	}
	if c.stats[t.what][t.name] == nil {
		c.stats[t.what][t.name] = map[string]*HostStat{}
	}
	stat := &HostStat{LastCheck: c.now().UnixMilli(), Version: t.version}
	healthy := c.healthy[t.name][t.version]
	if ok {
		stat.Healthy = true
		if !containsString(healthy, t.host) {
			c.healthy[t.name][t.version] = append(healthy, t.host)
		}
	} else {
		if prev := c.stats[t.what][t.name][t.host]; prev != nil && !prev.Healthy {
			stat.DownCount = prev.DownCount + 1
			stat.DownSince = prev.DownSince
		} else {
			stat.DownSince = stat.LastCheck
			stat.downFirst = true
		}
		if stat.DownCount <= reg.ServiceConfig.Awareness.MaxLogCount {
			c.logger.Error("Self Awareness health check for service [" + t.name + "] for host [" + t.host + "] is NOT healthy")
		}
		kept := healthy[:0:0]
		for _, h := range healthy {
			if h != t.host {
				kept = append(kept, h)
			}
		}
		c.healthy[t.name][t.version] = kept
	}
	c.stats[t.what][t.name][t.host] = stat
}

// GetHost takes the hosts of the service version in turn and returns the next healthy one
// (round-robin). It makes at most one pass over the hosts: Node.js recurses without a bound when
// none of them is in the healthy list.
func (c *Custom) GetHost(serviceName, version string) (*types.HostInfo, error) {
	reg := c.reg.Get()
	if reg == nil {
		return nil, ErrNoHost
	}
	if serviceName == "" {
		serviceName = c.gw.Name
	}
	svc := reg.Services[serviceName]
	if svc == nil || svc.Hosts == nil || svc.Hosts.Latest == "" {
		return nil, ErrNoHost
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.healthy[serviceName] == nil {
		return nil, ErrNoHost
	}
	if version == "" {
		version = svc.Hosts.Latest
	}
	hosts, ok := svc.Hosts.ByVersion[version]
	healthy := c.healthy[serviceName][version]
	if !ok || len(healthy) == 0 {
		return nil, ErrNoHost
	}
	for range hosts {
		i := c.indexes[serviceName][version]
		if i >= len(hosts) {
			i = 0
		}
		c.indexes[serviceName][version] = i + 1
		if containsString(healthy, hosts[i]) {
			return hostInfo(reg, serviceName, version, hosts[i]), nil
		}
	}
	return nil, ErrNoHost
}

// GetLatestVersion is the latest version of the service's hosts.
func (c *Custom) GetLatestVersion(serviceName string) string {
	reg := c.reg.Get()
	if reg == nil || reg.Services[serviceName] == nil || reg.Services[serviceName].Hosts == nil {
		return ""
	}
	return reg.Services[serviceName].Hosts.Latest
}

// GetLatestVersionFromCluster: there is no cluster without SOAJS_DEPLOY_HA.
func (c *Custom) GetLatestVersionFromCluster(string) (string, bool) { return "", false }

// Stats returns a copy of the hosts' health.
func (c *Custom) Stats() map[string]map[string]map[string]*HostStat {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := map[string]map[string]map[string]*HostStat{}
	for what, names := range c.stats {
		out[what] = map[string]map[string]*HostStat{}
		for name, hosts := range names {
			out[what][name] = map[string]*HostStat{}
			for h, s := range hosts {
				copy := *s
				out[what][name][h] = &copy
			}
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, e := range list {
		if e == s {
			return true
		}
	}
	return false
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
