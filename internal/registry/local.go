package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"

	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// LocalRegistry implements Registry using local file storage
type LocalRegistry struct {
	logger   *zap.Logger
	filePath string
	env      string

	mu   sync.RWMutex
	data *types.RegistryData
}

// NewLocalRegistry creates a new file-based registry
func NewLocalRegistry(filePath string, logger *zap.Logger) *LocalRegistry {
	return &LocalRegistry{
		filePath: filePath,
		logger:   logger,
	}
}

// Load loads the registry data from file
func (r *LocalRegistry) Load(env string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.env = env

	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return fmt.Errorf("failed to read registry file: %w", err)
	}

	var regData types.RegistryData
	if err := json.Unmarshal(data, &regData); err != nil {
		return fmt.Errorf("failed to parse registry file: %w", err)
	}

	r.data = &regData
	r.logger.Info("local registry loaded", zap.String("file", r.filePath))

	return nil
}

// Reload reloads the registry data
func (r *LocalRegistry) Reload() error {
	return r.Load(r.env)
}

// Get returns the current registry data
func (r *LocalRegistry) Get() *types.RegistryData {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.data
}

// GetService returns service configuration
func (r *LocalRegistry) GetService(name string) (*types.ServiceConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.Services == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	svc, exists := r.data.Services[name]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", name)
	}

	return svc, nil
}

// RegisterHost is not supported in local mode
func (r *LocalRegistry) RegisterHost(info types.HostInfo) error {
	r.logger.Warn("RegisterHost not supported in local mode")
	return nil
}

// GetHost returns a service host
func (r *LocalRegistry) GetHost(serviceName, version string) (*types.HostInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.Services == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	svc, exists := r.data.Services[serviceName]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", serviceName)
	}

	return &types.HostInfo{
		Name:    serviceName,
		Version: version,
		Host:    svc.Host,
		Port:    svc.Port,
	}, nil
}

// AddUpdateEnvControllers is not supported in local mode
func (r *LocalRegistry) AddUpdateEnvControllers(data types.ControllerUpdate) error {
	r.logger.Warn("AddUpdateEnvControllers not supported in local mode")
	return nil
}

// GetCustomRegistry returns a custom registry entry
func (r *LocalRegistry) GetCustomRegistry(name string) (interface{}, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.CustomRegistry == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	entry, exists := r.data.CustomRegistry[name]
	if !exists {
		return nil, fmt.Errorf("custom registry entry not found: %s", name)
	}

	return entry, nil
}

// GetResource returns a resource by type and name
func (r *LocalRegistry) GetResource(resourceType, name string) (*types.Resource, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.Resources == nil {
		return nil, fmt.Errorf("registry not loaded")
	}

	typeResources, exists := r.data.Resources[resourceType]
	if !exists {
		return nil, fmt.Errorf("resource type not found: %s", resourceType)
	}

	resource, exists := typeResources[name]
	if !exists {
		return nil, fmt.Errorf("resource not found: %s/%s", resourceType, name)
	}

	return resource, nil
}

// IsMaintenanceMode checks if the system is in maintenance mode
func (r *LocalRegistry) IsMaintenanceMode() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.data == nil || r.data.ServiceConfig == nil {
		return false
	}
	return r.data.ServiceConfig.MaintenanceMode
}

// GetEnvironment returns the current environment code
func (r *LocalRegistry) GetEnvironment() string {
	return r.env
}
