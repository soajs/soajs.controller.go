package registry

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/soajs/soajs.controller.go/pkg/types"
)

// Registry is the environment registry: loaded from the provision database, reloaded on demand
// and every services.config.awareness.autoRelaodRegistry ms.
type Registry interface {
	// Load loads the registry of the gateway's environment and starts the automatic reload.
	Load(env string) error

	// Reload loads the registry again. On error the previous registry stays in use.
	Reload() error

	// Get returns the current registry.
	Get() *types.RegistryData

	// LoadByEnv returns the registry of another environment, loaded once and cached; reload
	// loads it again.
	LoadByEnv(env string, reload bool) (*types.RegistryData, error)

	// RegisterHost records a service host in the hosts collection and in the registry.
	RegisterHost(host types.HostRecord) error

	// GetHost returns a host of a service: of the given version, or of the latest when version
	// is empty.
	GetHost(serviceName, version string) (*types.HostInfo, error)

	// GetEnvironment returns the gateway's environment code, lowercase.
	GetEnvironment() string

	// Close stops the automatic reload.
	Close()
}

// SensitiveEnvs are the environments whose tokens are accepted in every environment and whose
// hosts never override a service port: SOAJS_SENSITIVE_ENVS, a JSON array, else ["dashboard"].
func SensitiveEnvs() []string {
	if v := os.Getenv("SOAJS_SENSITIVE_ENVS"); v != "" {
		var envs []string
		if err := json.Unmarshal([]byte(v), &envs); err == nil && len(envs) > 0 {
			for i := range envs {
				envs[i] = strings.ToLower(envs[i])
			}
			return envs
		}
	}
	return []string{"dashboard"}
}

// IsSensitiveEnv reports whether env is one of SensitiveEnvs.
func IsSensitiveEnv(env string) bool {
	env = strings.ToLower(env)
	for _, e := range SensitiveEnvs() {
		if e == env {
			return true
		}
	}
	return false
}
