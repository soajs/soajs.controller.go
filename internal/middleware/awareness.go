package middleware

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/awareness"
	"github.com/soajs/soajs.controller.go/internal/registry"
)

// Awareness middleware injects service discovery capabilities
func Awareness(aw awareness.Awareness) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx != nil {
				soajsCtx.Awareness = &awarenessClientWrapper{aw: aw}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Global env hosts manager (singleton)
var globalEnvHosts *EnvHostsManager
var envHostsOnce sync.Once

// AwarenessEnv middleware injects environment-aware service discovery
func AwarenessEnv(aw awareness.Awareness, reg registry.Registry, currentEnv string) Middleware {
	// Initialize global env hosts manager
	envHostsOnce.Do(func() {
		globalEnvHosts = NewEnvHostsManager(reg)

		// Start health check if configured
		regData := reg.Get()
		if regData != nil && regData.ServiceConfig != nil {
			interval := time.Duration(regData.ServiceConfig.Awareness.HealthCheckInterval) * time.Millisecond
			if interval > 0 {
				controllerPort := regData.ServiceConfig.Ports.Controller
				maintenanceInc := regData.ServiceConfig.Ports.MaintenanceInc
				globalEnvHosts.StartHealthCheck(interval, controllerPort, maintenanceInc)
			}
		}
	})

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			soajsCtx := GetSOAJSContext(ctx)

			if soajsCtx != nil {
				soajsCtx.AwarenessEnv = &awarenessEnvClientWrapper{
					aw:         aw,
					reg:        reg,
					currentEnv: currentEnv,
					envHosts:   globalEnvHosts,
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// awarenessClientWrapper wraps awareness for the middleware interface
type awarenessClientWrapper struct {
	aw awareness.Awareness
}

func (w *awarenessClientWrapper) GetHost(serviceName string, version string) (string, error) {
	hostInfo, err := w.aw.GetHost(serviceName, version)
	if err != nil {
		return "", err
	}
	return hostInfo.Host, nil
}

// awarenessEnvClientWrapper wraps awareness for environment-aware lookups
type awarenessEnvClientWrapper struct {
	aw         awareness.Awareness
	reg        registry.Registry
	currentEnv string
	envHosts   *EnvHostsManager
}

func (w *awarenessEnvClientWrapper) GetHost(env string, serviceName string, version string) (string, error) {
	// For current environment, use the standard awareness
	if env == "" || env == w.currentEnv {
		hostInfo, err := w.aw.GetHost(serviceName, version)
		if err != nil {
			return "", err
		}
		return hostInfo.Host, nil
	}

	// Check if running in HA mode
	deployHA := os.Getenv("SOAJS_DEPLOY_HA")
	if deployHA != "" {
		// In HA mode, build DNS hostname for the target environment
		// Format: {env}-{serviceName}-v{version}-service
		// This matches Node.js behavior in mw/awarenessEnv/ha.js
		if version == "" {
			version = "1"
		}
		hostname := fmt.Sprintf("%s-%s-v%s-service", strings.ToLower(env), serviceName, version)
		return hostname, nil
	}

	// For other environments in non-HA mode, use envHosts manager
	if w.envHosts != nil {
		return w.envHosts.GetHost(env, serviceName, version)
	}

	return "", fmt.Errorf("environment %s not available", env)
}

// EnvHostsManager manages controller hosts for multiple environments
type EnvHostsManager struct {
	mu              sync.RWMutex
	hosts           map[string]map[string]*EnvServiceInfo // env -> service -> info
	reg             registry.Registry
	healthCheckDone bool
}

// EnvServiceInfo holds service info for an environment
type EnvServiceInfo struct {
	Healthy map[string][]string // version -> []host
	Indexes map[string]int      // version -> current index
	Latest  string
}

// NewEnvHostsManager creates a new environment hosts manager
func NewEnvHostsManager(reg registry.Registry) *EnvHostsManager {
	return &EnvHostsManager{
		hosts: make(map[string]map[string]*EnvServiceInfo),
		reg:   reg,
	}
}

// GetHost returns a healthy host for the given environment/service/version using round-robin
func (m *EnvHostsManager) GetHost(env, serviceName, version string) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	envServices, ok := m.hosts[env]
	if !ok {
		return "", fmt.Errorf("no hosts for environment %s", env)
	}

	svcInfo, ok := envServices[serviceName]
	if !ok {
		return "", fmt.Errorf("no hosts for service %s in environment %s", serviceName, env)
	}

	// Use latest version if not specified
	if version == "" {
		version = svcInfo.Latest
	}

	healthyHosts, ok := svcInfo.Healthy[version]
	if !ok || len(healthyHosts) == 0 {
		return "", fmt.Errorf("no healthy hosts for %s@%s in environment %s", serviceName, version, env)
	}

	// Round-robin
	index := svcInfo.Indexes[version]
	if index >= len(healthyHosts) {
		index = 0
	}
	host := healthyHosts[index]
	svcInfo.Indexes[version] = index + 1

	return host, nil
}

// AddHost adds a host for a service in an environment
func (m *EnvHostsManager) AddHost(env, serviceName, version, host string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.hosts[env] == nil {
		m.hosts[env] = make(map[string]*EnvServiceInfo)
	}

	if m.hosts[env][serviceName] == nil {
		m.hosts[env][serviceName] = &EnvServiceInfo{
			Healthy: make(map[string][]string),
			Indexes: make(map[string]int),
			Latest:  version,
		}
	}

	svcInfo := m.hosts[env][serviceName]
	if svcInfo.Healthy[version] == nil {
		svcInfo.Healthy[version] = []string{}
	}

	// Check if host already exists
	for _, h := range svcInfo.Healthy[version] {
		if h == host {
			return
		}
	}

	svcInfo.Healthy[version] = append(svcInfo.Healthy[version], host)

	// Update latest version
	if version > svcInfo.Latest {
		svcInfo.Latest = version
	}
}

// RemoveHost removes a host for a service in an environment
func (m *EnvHostsManager) RemoveHost(env, serviceName, version, host string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.hosts[env] == nil || m.hosts[env][serviceName] == nil {
		return
	}

	svcInfo := m.hosts[env][serviceName]
	if svcInfo.Healthy[version] == nil {
		return
	}

	// Remove host from list
	for i, h := range svcInfo.Healthy[version] {
		if h == host {
			svcInfo.Healthy[version] = append(svcInfo.Healthy[version][:i], svcInfo.Healthy[version][i+1:]...)
			break
		}
	}
}

// StartHealthCheck starts periodic health checking of hosts
func (m *EnvHostsManager) StartHealthCheck(interval time.Duration, controllerPort, maintenanceInc int) {
	if m.healthCheckDone {
		return
	}
	m.healthCheckDone = true

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for range ticker.C {
			m.checkAllHosts(controllerPort, maintenanceInc)
		}
	}()
}

// checkAllHosts checks health of all registered hosts
func (m *EnvHostsManager) checkAllHosts(controllerPort, maintenanceInc int) {
	m.mu.RLock()
	// Create a copy of hosts to check
	hostsToCheck := make([]struct {
		env, svc, ver, host string
	}, 0)

	for env, services := range m.hosts {
		for svc, info := range services {
			for ver, hosts := range info.Healthy {
				for _, host := range hosts {
					hostsToCheck = append(hostsToCheck, struct {
						env, svc, ver, host string
					}{env, svc, ver, host})
				}
			}
		}
	}
	m.mu.RUnlock()

	// Check each host
	for _, h := range hostsToCheck {
		maintenancePort := controllerPort + maintenanceInc
		url := fmt.Sprintf("http://%s:%d/heartbeat", h.host, maintenancePort)

		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != 200 {
			m.RemoveHost(h.env, h.svc, h.ver, h.host)
		}
		if resp != nil {
			resp.Body.Close()
		}
	}
}
