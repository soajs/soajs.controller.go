package awareness

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// mockRegistry implements registry.Registry for testing
type mockRegistry struct {
	hosts       map[string]*types.HostInfo
	registerErr error
}

func newMockRegistry() *mockRegistry {
	return &mockRegistry{
		hosts: make(map[string]*types.HostInfo),
	}
}

func (m *mockRegistry) Load(env string) error                                    { return nil }
func (m *mockRegistry) Reload() error                                            { return nil }
func (m *mockRegistry) Get() *types.RegistryData                                 { return nil }
func (m *mockRegistry) GetService(name string) (*types.ServiceConfig, error)     { return nil, nil }
func (m *mockRegistry) GetCustomRegistry(name string) (interface{}, error)       { return nil, nil }
func (m *mockRegistry) GetResource(resourceType, name string) (*types.Resource, error) {
	return nil, nil
}
func (m *mockRegistry) IsMaintenanceMode() bool                                  { return false }
func (m *mockRegistry) GetEnvironment() string                                   { return "dev" }
func (m *mockRegistry) AddUpdateEnvControllers(data types.ControllerUpdate) error {
	return nil
}

func (m *mockRegistry) GetHost(serviceName, version string) (*types.HostInfo, error) {
	key := serviceName + ":" + version
	if host, ok := m.hosts[key]; ok {
		return host, nil
	}
	return nil, nil
}

func (m *mockRegistry) RegisterHost(info types.HostInfo) error {
	if m.registerErr != nil {
		return m.registerErr
	}
	key := info.Name + ":" + info.Version
	m.hosts[key] = &info
	return nil
}

func TestNewAwareness(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()
	cfg := config.AwarenessConfig{
		HealthCheckInterval: 0, // Disable health check loop for testing
	}

	awareness := NewAwareness(cfg, reg, logger)

	if awareness == nil {
		t.Fatal("NewAwareness returned nil")
	}
	if awareness.hosts == nil {
		t.Error("hosts map is nil")
	}
}

func TestDefaultAwareness_RegisterAndGetHost(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()
	cfg := config.AwarenessConfig{}

	awareness := NewAwareness(cfg, reg, logger)

	// Register a service
	info := types.ServiceInfo{
		Name:    "urac",
		Version: "2",
		Host:    "127.0.0.1",
		Port:    4001,
	}
	err := awareness.RegisterService(info)
	if err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	// Get the host
	host, err := awareness.GetHost("urac", "2")
	if err != nil {
		t.Fatalf("GetHost failed: %v", err)
	}
	if host == nil {
		t.Fatal("GetHost returned nil")
	}
	if host.Host != "127.0.0.1" {
		t.Errorf("Host = %s, want 127.0.0.1", host.Host)
	}
	if host.Port != 4001 {
		t.Errorf("Port = %d, want 4001", host.Port)
	}
}

func TestDefaultAwareness_DeregisterService(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()
	cfg := config.AwarenessConfig{}

	awareness := NewAwareness(cfg, reg, logger)

	// Register two hosts for same service
	awareness.RegisterService(types.ServiceInfo{
		Name: "urac", Version: "2", Host: "127.0.0.1", Port: 4001,
	})
	awareness.RegisterService(types.ServiceInfo{
		Name: "urac", Version: "2", Host: "127.0.0.2", Port: 4001,
	})

	// Verify both registered
	stats := awareness.GetStats()
	if len(stats.Services["urac:2"].Hosts) != 2 {
		t.Errorf("Expected 2 hosts, got %d", len(stats.Services["urac:2"].Hosts))
	}

	// Deregister first host
	awareness.DeregisterService(types.ServiceInfo{
		Name: "urac", Version: "2", Host: "127.0.0.1", Port: 4001,
	})

	// Verify only one remains
	stats = awareness.GetStats()
	if len(stats.Services["urac:2"].Hosts) != 1 {
		t.Errorf("Expected 1 host after deregister, got %d", len(stats.Services["urac:2"].Hosts))
	}
	if stats.Services["urac:2"].Hosts[0].Host != "127.0.0.2" {
		t.Error("Wrong host remaining after deregister")
	}
}

func TestDefaultAwareness_GetStats(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()
	cfg := config.AwarenessConfig{}

	awareness := NewAwareness(cfg, reg, logger)

	// Register services
	awareness.RegisterService(types.ServiceInfo{
		Name: "urac", Version: "2", Host: "127.0.0.1", Port: 4001,
	})
	awareness.RegisterService(types.ServiceInfo{
		Name: "oauth", Version: "1", Host: "127.0.0.1", Port: 4002,
	})

	stats := awareness.GetStats()

	if stats == nil {
		t.Fatal("GetStats returned nil")
	}
	if len(stats.Services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(stats.Services))
	}
	if stats.TS == 0 {
		t.Error("TS should not be zero")
	}

	// Check urac service
	uracStats, ok := stats.Services["urac:2"]
	if !ok {
		t.Fatal("urac:2 not in stats")
	}
	if uracStats.Healthy != 1 {
		t.Errorf("Expected 1 healthy host, got %d", uracStats.Healthy)
	}
}

func TestDefaultAwareness_GetHostFallbackToRegistry(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()

	// Add host directly to registry (not via awareness)
	reg.hosts["oauth:1"] = &types.HostInfo{
		Name:    "oauth",
		Version: "1",
		Host:    "192.168.1.100",
		Port:    4002,
	}

	cfg := config.AwarenessConfig{}
	awareness := NewAwareness(cfg, reg, logger)

	// Should fall back to registry
	host, err := awareness.GetHost("oauth", "1")
	if err != nil {
		t.Fatalf("GetHost failed: %v", err)
	}
	if host == nil {
		t.Fatal("GetHost returned nil")
	}
	if host.Host != "192.168.1.100" {
		t.Errorf("Host = %s, want 192.168.1.100", host.Host)
	}
}

func TestDefaultAwareness_HealthCheck(t *testing.T) {
	// Create a test server that returns healthy
	healthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/heartbeat" {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"result":true}`))
		}
	}))
	defer healthyServer.Close()

	// Create a test server that returns unhealthy
	unhealthyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer unhealthyServer.Close()

	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()
	cfg := config.AwarenessConfig{
		HealthCheckTimeout: 5000,
	}
	awareness := NewAwareness(cfg, reg, logger)

	t.Run("healthy service", func(t *testing.T) {
		// Extract host:port from test server URL
		host := strings.TrimPrefix(healthyServer.URL, "http://")

		status, err := awareness.HealthCheck("test", host)
		if err != nil {
			t.Fatalf("HealthCheck failed: %v", err)
		}
		if !status.Healthy {
			t.Error("Expected healthy=true")
		}
		if status.Latency == 0 {
			t.Error("Latency should not be zero")
		}
	})

	t.Run("unhealthy service", func(t *testing.T) {
		host := strings.TrimPrefix(unhealthyServer.URL, "http://")

		status, err := awareness.HealthCheck("test", host)
		if err != nil {
			t.Fatalf("HealthCheck failed: %v", err)
		}
		if status.Healthy {
			t.Error("Expected healthy=false for 503 status")
		}
		if status.Error == "" {
			t.Error("Expected error message for unhealthy service")
		}
	})

	t.Run("unreachable service", func(t *testing.T) {
		status, err := awareness.HealthCheck("test", "127.0.0.1:59999")
		if err != nil {
			t.Fatalf("HealthCheck should not return error: %v", err)
		}
		if status.Healthy {
			t.Error("Expected healthy=false for unreachable service")
		}
		if status.Error == "" {
			t.Error("Expected error message")
		}
	})
}

func TestDefaultAwareness_Concurrent(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()
	cfg := config.AwarenessConfig{}
	awareness := NewAwareness(cfg, reg, logger)

	var wg sync.WaitGroup

	// Concurrent registrations
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			awareness.RegisterService(types.ServiceInfo{
				Name:    "concurrent",
				Version: "1",
				Host:    "127.0.0.1",
				Port:    4000 + n,
			})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			awareness.GetHost("concurrent", "1")
			awareness.GetStats()
		}()
	}

	wg.Wait()
	// If we get here without deadlock or panic, test passes
}

func TestDefaultAwareness_GetHostUnhealthySkipped(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	reg := newMockRegistry()
	cfg := config.AwarenessConfig{}
	awareness := NewAwareness(cfg, reg, logger)

	// Register host and manually mark as unhealthy
	awareness.RegisterService(types.ServiceInfo{
		Name: "test", Version: "1", Host: "127.0.0.1", Port: 4001,
	})

	// Manually mark as unhealthy
	awareness.mu.Lock()
	if entries, ok := awareness.hosts["test:1"]; ok && len(entries) > 0 {
		entries[0].healthy = false
	}
	awareness.mu.Unlock()

	// Add a healthy fallback in registry
	reg.hosts["test:1"] = &types.HostInfo{
		Name: "test", Version: "1", Host: "192.168.1.1", Port: 4001,
	}

	// Should fall back to registry since local cache is unhealthy
	host, err := awareness.GetHost("test", "1")
	if err != nil {
		t.Fatalf("GetHost failed: %v", err)
	}
	if host.Host != "192.168.1.1" {
		t.Errorf("Expected fallback host 192.168.1.1, got %s", host.Host)
	}
}
