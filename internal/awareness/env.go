package awareness

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/lib"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// EnvAwareness finds the gateway's controllers in other environments.
type EnvAwareness interface {
	// GetHost returns a host of the gateway in env (the gateway's own environment when ""), of
	// version, of the latest when version is "". "" when there is none.
	GetHost(env, version string) string
}

// EnvCustom is the awarenessEnv without SOAJS_DEPLOY_HA (mw/awarenessEnv/custom.js): the
// gateway's hosts of the other environments, read from the hosts collection when the registry
// loads and every autoRelaodRegistry, checked on /heartbeat at this gateway's maintenance port
// every healthCheckInterval, and taken in turn.
type EnvCustom struct {
	reg    registry.Registry
	gw     Gateway
	logger *zap.Logger
	client *http.Client
	env    string

	mu         sync.Mutex
	hosts      []types.HostRecord
	loadedTime int64
	healthy    map[string]map[string][]string // env, version
	indexes    map[string]map[string]int
	latest     map[string]string
}

// NewEnvCustom returns the registry-hosts awareness of the other environments; Start begins the
// checks.
func NewEnvCustom(reg registry.Registry, gw Gateway, logger *zap.Logger) *EnvCustom {
	return &EnvCustom{
		reg: reg, gw: gw, logger: logger, env: reg.GetEnvironment(),
		client:  &http.Client{Timeout: checkTimeout},
		healthy: map[string]map[string][]string{},
		indexes: map[string]map[string]int{},
		latest:  map[string]string{},
	}
}

// Start reloads the hosts every autoRelaodRegistry ms and checks them every healthCheckInterval
// ms, until ctx is done.
func (e *EnvCustom) Start(ctx context.Context) {
	reg := e.reg.Get()
	if reg == nil || reg.ServiceConfig == nil {
		return
	}
	aw := reg.ServiceConfig.Awareness
	if aw.AutoRelaodRegistry > 0 {
		go e.every(ctx, func(r *types.RegistryData) int { return r.ServiceConfig.Awareness.AutoRelaodRegistry }, true, e.Fetch)
	}
	if aw.HealthCheckInterval > 0 {
		go e.every(ctx, func(r *types.RegistryData) int { return r.ServiceConfig.Awareness.HealthCheckInterval }, false, e.Check)
	}
}

func (e *EnvCustom) every(ctx context.Context, interval func(*types.RegistryData) int, waitFirst bool, fn func()) {
	for {
		if !waitFirst {
			fn()
		}
		waitFirst = false
		ms := 0
		if r := e.reg.Get(); r != nil && r.ServiceConfig != nil {
			ms = interval(r)
		}
		if ms <= 0 {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(ms) * time.Millisecond):
		}
	}
}

// Fetch reloads the gateway's hosts of the other environments; on error the previous ones stay.
func (e *EnvCustom) Fetch() {
	hosts, err := e.reg.OtherEnvControllerHosts(e.gw.Name)
	if err != nil {
		e.logger.Error("Failed to load controller hosts. reusing from previous load. Reason: " + err.Error())
		return
	}
	e.mu.Lock()
	e.hosts = hosts
	e.mu.Unlock()
}

// Check checks each host, after reloading them when the registry was loaded again.
func (e *EnvCustom) Check() {
	reg := e.reg.Get()
	if reg == nil || reg.ServiceConfig == nil {
		return
	}
	e.mu.Lock()
	reload := e.loadedTime != reg.TimeLoaded
	e.loadedTime = reg.TimeLoaded
	e.mu.Unlock()
	if reload {
		e.Fetch()
	}
	port := reg.ServiceConfig.Ports.MaintenanceInc
	if gw := reg.Services[e.gw.Name]; gw != nil {
		port += gw.Port
	}
	e.mu.Lock()
	hosts := append([]types.HostRecord(nil), e.hosts...)
	for _, h := range hosts {
		if e.healthy[h.Env] == nil {
			e.healthy[h.Env] = map[string][]string{}
			e.indexes[h.Env] = map[string]int{}
			e.latest[h.Env] = "1"
		}
		if e.healthy[h.Env][h.Version] == nil {
			e.healthy[h.Env][h.Version] = []string{}
		}
		if lib.IsLatest(h.Version, e.latest[h.Env]) {
			e.latest[h.Env] = h.Version
		}
	}
	e.mu.Unlock()

	var wg sync.WaitGroup
	for _, h := range hosts {
		wg.Add(1)
		go func(h types.HostRecord) {
			defer wg.Done()
			ok := false
			if resp, err := e.client.Get(fmt.Sprintf("http://%s:%d/heartbeat", h.IP, port)); err == nil {
				resp.Body.Close()
				ok = resp.StatusCode == http.StatusOK
			}
			e.mu.Lock()
			defer e.mu.Unlock()
			list := e.healthy[h.Env][h.Version]
			if ok {
				if !containsString(list, h.IP) {
					e.healthy[h.Env][h.Version] = append(list, h.IP)
				}
				return
			}
			kept := list[:0:0]
			for _, ip := range list {
				if ip != h.IP {
					kept = append(kept, ip)
				}
			}
			e.healthy[h.Env][h.Version] = kept
		}(h)
	}
	wg.Wait()
}

// GetHost takes the healthy hosts of the gateway in env in turn.
func (e *EnvCustom) GetHost(env, version string) string {
	if env == "" {
		env = e.env
	}
	env = strings.ToLower(env)
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.healthy[env] == nil {
		return ""
	}
	if version == "" {
		version = e.latest[env]
	}
	list := e.healthy[env][version]
	if len(list) == 0 {
		return ""
	}
	i := e.indexes[env][version]
	if i >= len(list) {
		i = 0
	}
	e.indexes[env][version] = i + 1
	return list[i]
}

// EnvHA is the awarenessEnv with SOAJS_DEPLOY_HA (mw/awarenessEnv/ha.js): the gateway's
// Kubernetes service name in the other environment, in that environment's namespace. Node.js
// reads the namespace from the gateway's own deployer in the old shape; here it is the target
// environment's deployer.container.kubernetes.namespace, and without it there is no host.
type EnvHA struct {
	reg registry.Registry
	gw  Gateway
	env string
}

// NewEnvHA returns the Kubernetes awareness of the other environments.
func NewEnvHA(reg registry.Registry, gw Gateway) *EnvHA {
	return &EnvHA{reg: reg, gw: gw, env: reg.GetEnvironment()}
}

// GetHost returns <env>-<gateway>[-v<version>-service].<namespace of env>; the version is the
// gateway's own, as Node.js does.
func (e *EnvHA) GetHost(env, _ string) string {
	if env == "" {
		env = e.env
	}
	env = strings.ToLower(env)
	var target *types.RegistryData
	if env == e.env {
		target = e.reg.Get()
	} else {
		r, err := e.reg.LoadByEnv(env, false)
		if err != nil {
			return ""
		}
		target = r
	}
	return gatewayHost(env, e.gw, target)
}
