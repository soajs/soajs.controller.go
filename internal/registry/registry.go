package registry

import "github.com/soajs/soajs.controller.go/pkg/types"

// Registry interface for service configuration and discovery
type Registry interface {
	// Load loads the registry data for the given environment
	Load(env string) error

	// Reload reloads the registry data
	Reload() error

	// Get returns the current registry data
	Get() *types.RegistryData

	// GetService returns service configuration
	GetService(name string) (*types.ServiceConfig, error)

	// GetCustomRegistry returns a custom registry entry
	GetCustomRegistry(name string) (*types.CustomEntry, error)

	// GetResource returns a resource by type and name
	GetResource(resourceType, name string) (*types.Resource, error)

	// RegisterHost registers a service host
	RegisterHost(info types.HostInfo) error

	// GetHost returns a service host
	GetHost(serviceName, version string) (*types.HostInfo, error)

	// AddUpdateEnvControllers updates environment controller info
	AddUpdateEnvControllers(data types.ControllerUpdate) error

	// IsMaintenanceMode checks if the system is in maintenance mode
	IsMaintenanceMode() bool

	// GetEnvironment returns the current environment code
	GetEnvironment() string
}
