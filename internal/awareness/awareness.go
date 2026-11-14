// Package awareness finds the hosts of services as the Node.js gateway does (mw/awareness,
// mw/awarenessEnv): without SOAJS_DEPLOY_HA, from the registry's hosts, health-checked and taken
// in turn; with it, from Kubernetes.
package awareness

import (
	"errors"
	"os"

	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// ErrNoHost is returned when awareness has no host for a service.
var ErrNoHost = errors.New("no host")

// Awareness finds the hosts of services in the gateway's environment.
type Awareness interface {
	// GetHost returns a host of a service version, of its latest version when version is "".
	// The port is the service's port in the registry.
	GetHost(serviceName, version string) (*types.HostInfo, error)

	// GetLatestVersion returns the latest version of a service, "" when unknown.
	GetLatestVersion(serviceName string) string

	// GetLatestVersionFromCluster asks the cluster for the latest version of a service; ok is
	// false without SOAJS_DEPLOY_HA, where there is no cluster to ask.
	GetLatestVersionFromCluster(serviceName string) (version string, ok bool)

	// Stats returns the health of each host, by "services" or "daemons", then name, then host;
	// nil in HA mode.
	Stats() map[string]map[string]map[string]*HostStat
}

// Gateway names the gateway itself: its service name and version.
type Gateway struct {
	Name    string
	Version string
}

// New returns the awareness of the gateway's environment: Kubernetes when SOAJS_DEPLOY_HA is
// set, the registry's hosts otherwise.
func New(reg registry.Registry, gw Gateway, logger *zap.Logger) Awareness {
	if os.Getenv("SOAJS_DEPLOY_HA") != "" {
		return NewHA(reg, gw, logger, nil)
	}
	return NewCustom(reg, gw, logger)
}

// NewEnv returns the awareness of the gateway's controllers in other environments.
func NewEnv(reg registry.Registry, gw Gateway, logger *zap.Logger) EnvAwareness {
	if os.Getenv("SOAJS_DEPLOY_HA") != "" {
		return NewEnvHA(reg, gw)
	}
	return NewEnvCustom(reg, gw, logger)
}

func hostInfo(reg *types.RegistryData, name, version, host string) *types.HostInfo {
	info := &types.HostInfo{Name: name, Version: version, Host: host}
	if reg != nil {
		if svc := reg.Services[name]; svc != nil {
			info.Port = svc.Port
		}
	}
	return info
}
