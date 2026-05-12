package awareness

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// Awareness interface for service discovery
type Awareness interface {
	// GetHost returns a healthy host for the given service
	GetHost(serviceName, version string) (*types.HostInfo, error)

	// HealthCheck performs a health check on a service host
	HealthCheck(serviceName, host string) (*HealthStatus, error)

	// RegisterService registers a service host
	RegisterService(info types.ServiceInfo) error

	// DeregisterService removes a service host
	DeregisterService(info types.ServiceInfo) error

	// GetStats returns awareness statistics
	GetStats() *AwarenessStats
}

// HealthStatus represents the health of a service host
type HealthStatus struct {
	Healthy   bool
	LastCheck time.Time
	Latency   time.Duration
	Error     string
}

// AwarenessStats holds awareness statistics
type AwarenessStats struct {
	Services map[string]*ServiceStats `json:"services"`
	TS       int64                    `json:"ts"`
}

// ServiceStats holds per-service statistics
type ServiceStats struct {
	Hosts     []HostStats `json:"hosts"`
	Healthy   int         `json:"healthy"`
	Unhealthy int         `json:"unhealthy"`
}

// HostStats holds per-host statistics
type HostStats struct {
	Host      string    `json:"host"`
	Port      int       `json:"port"`
	Healthy   bool      `json:"healthy"`
	LastCheck time.Time `json:"lastCheck"`
}

// DefaultAwareness implements Awareness using registry
type DefaultAwareness struct {
	cfg      config.AwarenessConfig
	registry registry.Registry
	logger   *zap.Logger

	mu    sync.RWMutex
	hosts map[string][]*hostEntry
}

type hostEntry struct {
	info      *types.HostInfo
	healthy   bool
	lastCheck time.Time
	latency   time.Duration
}

// NewAwareness creates a new awareness instance
func NewAwareness(cfg config.AwarenessConfig, reg registry.Registry, logger *zap.Logger) *DefaultAwareness {
	a := &DefaultAwareness{
		cfg:      cfg,
		registry: reg,
		logger:   logger,
		hosts:    make(map[string][]*hostEntry),
	}

	// Start health check loop
	if cfg.HealthCheckInterval > 0 {
		go a.healthCheckLoop()
	}

	return a
}

// GetHost returns a healthy host for the given service
func (a *DefaultAwareness) GetHost(serviceName, version string) (*types.HostInfo, error) {
	// First check local cache
	key := serviceName + ":" + version
	a.mu.RLock()
	entries, exists := a.hosts[key]
	a.mu.RUnlock()

	if exists && len(entries) > 0 {
		// Return first healthy host (simple round-robin could be added)
		for _, entry := range entries {
			if entry.healthy {
				return entry.info, nil
			}
		}
	}

	// Fall back to registry
	hostInfo, err := a.registry.GetHost(serviceName, version)
	if err != nil {
		return nil, err
	}

	return hostInfo, nil
}

// HealthCheck performs a health check on a service host
func (a *DefaultAwareness) HealthCheck(serviceName, host string) (*HealthStatus, error) {
	start := time.Now()
	status := &HealthStatus{
		LastCheck: start,
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: time.Duration(a.cfg.HealthCheckTimeout) * time.Millisecond,
	}

	// Build health check URL
	healthURL := fmt.Sprintf("http://%s/heartbeat", host)

	// Perform health check
	resp, err := client.Get(healthURL)
	if err != nil {
		status.Healthy = false
		status.Error = err.Error()
		status.Latency = time.Since(start)
		return status, nil
	}
	defer resp.Body.Close()

	status.Latency = time.Since(start)

	// Check response status code
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		status.Healthy = true
	} else {
		status.Healthy = false
		status.Error = fmt.Sprintf("unhealthy status code: %d", resp.StatusCode)
	}

	return status, nil
}

// RegisterService registers a service host
func (a *DefaultAwareness) RegisterService(info types.ServiceInfo) error {
	key := info.Name + ":" + info.Version

	a.mu.Lock()
	defer a.mu.Unlock()

	entry := &hostEntry{
		info: &types.HostInfo{
			Name:    info.Name,
			Version: info.Version,
			Host:    info.Host,
			Port:    info.Port,
		},
		healthy:   true,
		lastCheck: time.Now(),
	}

	a.hosts[key] = append(a.hosts[key], entry)

	// Also register with registry
	return a.registry.RegisterHost(*entry.info)
}

// DeregisterService removes a service host
func (a *DefaultAwareness) DeregisterService(info types.ServiceInfo) error {
	key := info.Name + ":" + info.Version

	a.mu.Lock()
	defer a.mu.Unlock()

	entries := a.hosts[key]
	var newEntries []*hostEntry
	for _, entry := range entries {
		if entry.info.Host != info.Host || entry.info.Port != info.Port {
			newEntries = append(newEntries, entry)
		}
	}
	a.hosts[key] = newEntries

	return nil
}

// GetStats returns awareness statistics
func (a *DefaultAwareness) GetStats() *AwarenessStats {
	a.mu.RLock()
	defer a.mu.RUnlock()

	stats := &AwarenessStats{
		Services: make(map[string]*ServiceStats),
		TS:       time.Now().UnixMilli(),
	}

	for key, entries := range a.hosts {
		svcStats := &ServiceStats{
			Hosts: make([]HostStats, 0, len(entries)),
		}

		for _, entry := range entries {
			svcStats.Hosts = append(svcStats.Hosts, HostStats{
				Host:      entry.info.Host,
				Port:      entry.info.Port,
				Healthy:   entry.healthy,
				LastCheck: entry.lastCheck,
			})

			if entry.healthy {
				svcStats.Healthy++
			} else {
				svcStats.Unhealthy++
			}
		}

		stats.Services[key] = svcStats
	}

	return stats
}

func (a *DefaultAwareness) healthCheckLoop() {
	ticker := time.NewTicker(time.Duration(a.cfg.HealthCheckInterval) * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		a.performHealthChecks()
	}
}

func (a *DefaultAwareness) performHealthChecks() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, entries := range a.hosts {
		for _, entry := range entries {
			status, err := a.HealthCheck(entry.info.Name, fmt.Sprintf("%s:%d", entry.info.Host, entry.info.Port))
			if err != nil {
				entry.healthy = false
				entry.lastCheck = time.Now()
				a.logger.Warn("health check failed",
					zap.String("service", entry.info.Name),
					zap.String("host", entry.info.Host),
					zap.Error(err),
				)
			} else {
				entry.healthy = status.Healthy
				entry.lastCheck = status.LastCheck
				entry.latency = status.Latency
			}
		}
	}
}
