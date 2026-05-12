package middleware

import (
	"net/http"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/registry"
)

// MaintenanceMode middleware checks if the system is in maintenance mode
func MaintenanceMode(reg registry.Registry) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			regData := reg.Get()

			// Check global maintenance mode
			if regData != nil && regData.ServiceConfig != nil {
				if regData.ServiceConfig.MaintenanceMode {
					WriteError(w, http.StatusServiceUnavailable,
						config.ErrCodeMaintenanceMode,
						config.DefaultErrors[config.ErrCodeMaintenanceMode])
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
