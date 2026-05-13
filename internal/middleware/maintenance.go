package middleware

import (
	"net/http"
	"strconv"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// MaintenanceModeConfig holds maintenance mode configuration from custom registry
type MaintenanceModeConfig struct {
	On         bool   `json:"on"`
	Status     int    `json:"status"`
	Message    string `json:"message"`
	RetryAfter int    `json:"retryAfter"`
}

// MaintenanceMode middleware checks if the system is in maintenance mode
func MaintenanceMode(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reg == nil {
				next.ServeHTTP(w, r)
				return
			}

			regData := reg.Get()
			if regData == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Check custom registry maintenance mode first (higher priority)
			// Path: registry.custom.gateway.value.maintenanceMode
			customConfig := getMaintenanceModeConfig(regData)
			if customConfig != nil && customConfig.On {
				status := customConfig.Status
				if status == 0 {
					status = http.StatusServiceUnavailable
				}
				message := customConfig.Message
				if message == "" {
					message = config.DefaultErrors[config.ErrCodeMaintenanceMode]
				}

				// Set Retry-After header if configured
				if customConfig.RetryAfter > 0 {
					w.Header().Set("Retry-After", strconv.Itoa(customConfig.RetryAfter))
				}

				w.WriteHeader(status)
				WriteError(w, status, config.ErrCodeMaintenanceMode, message)
				return
			}

			// Fall back to serviceConfig maintenance mode
			if regData.ServiceConfig != nil && regData.ServiceConfig.MaintenanceMode {
				WriteError(w, http.StatusServiceUnavailable,
					config.ErrCodeMaintenanceMode,
					config.DefaultErrors[config.ErrCodeMaintenanceMode])
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// getMaintenanceModeConfig extracts maintenance mode config from custom registry
// Path: registry.custom.gateway.value.maintenanceMode
func getMaintenanceModeConfig(regData *types.RegistryData) *MaintenanceModeConfig {
	if regData == nil || regData.CustomRegistry == nil {
		return nil
	}

	gateway, ok := regData.CustomRegistry["gateway"]
	if !ok {
		return nil
	}

	var gatewayValue interface{}
	switch v := gateway.(type) {
	case map[string]interface{}:
		if val, exists := v["value"]; exists {
			gatewayValue = val
		} else {
			gatewayValue = v
		}
	default:
		return nil
	}

	gatewayMap, ok := gatewayValue.(map[string]interface{})
	if !ok {
		return nil
	}

	mmData, ok := gatewayMap["maintenanceMode"].(map[string]interface{})
	if !ok {
		return nil
	}

	cfg := &MaintenanceModeConfig{}

	if on, ok := mmData["on"].(bool); ok {
		cfg.On = on
	}
	if status, ok := mmData["status"].(float64); ok {
		cfg.Status = int(status)
	}
	if message, ok := mmData["message"].(string); ok {
		cfg.Message = message
	}
	if retryAfter, ok := mmData["retryAfter"].(float64); ok {
		cfg.RetryAfter = int(retryAfter)
	}

	return cfg
}
