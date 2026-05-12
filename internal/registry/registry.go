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

	// RegisterHost registers a service host
	RegisterHost(info types.HostInfo) error

	// GetHost returns a service host
	GetHost(serviceName, version string) (*types.HostInfo, error)

	// AddUpdateEnvControllers updates environment controller info
	AddUpdateEnvControllers(data types.ControllerUpdate) error
}
