package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.uber.org/zap"
)

// Server represents the HTTP server
type Server struct {
	cfg        *config.Config
	logger     *zap.Logger
	router     *chi.Mux
	httpServer *http.Server
	maintServer *http.Server
}

// New creates a new server instance
func New(cfg *config.Config, logger *zap.Logger) *Server {
	s := &Server{
		cfg:    cfg,
		logger: logger,
		router: chi.NewRouter(),
	}

	return s
}

// Run starts the server and blocks until context is cancelled
func (s *Server) Run(ctx context.Context) error {
	// Initialize controller
	ctrl, err := NewController(s.cfg, s.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize controller: %w", err)
	}

	// Setup middleware and routes
	if err := ctrl.Setup(s.router); err != nil {
		return fmt.Errorf("failed to setup controller: %w", err)
	}

	// Create HTTP server
	s.httpServer = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.cfg.ServicePort),
		Handler:      s.router,
		ReadTimeout:  time.Duration(s.cfg.Server.ReadTimeout) * time.Second,
		WriteTimeout: time.Duration(s.cfg.Server.WriteTimeout) * time.Second,
		IdleTimeout:  time.Duration(s.cfg.Server.IdleTimeout) * time.Second,
	}

	// Create maintenance server using registry values
	// maintPort = registry.services.controller.port + registry.serviceConfig.ports.maintenanceInc
	maintPort := s.cfg.ServicePort + s.cfg.Maintenance.PortIncrement // fallback
	regData := ctrl.Registry().Get()
	if regData != nil && regData.ServiceConfig != nil {
		controllerPort := regData.ServiceConfig.Ports.Controller
		maintenanceInc := regData.ServiceConfig.Ports.MaintenanceInc
		if controllerPort > 0 && maintenanceInc > 0 {
			maintPort = controllerPort + maintenanceInc
		}
	}
	s.maintServer = NewMaintenanceServer(maintPort, ctrl, s.logger)

	// Start servers
	errChan := make(chan error, 2)

	go func() {
		s.logger.Info("starting HTTP server",
			zap.String("service", s.cfg.ServiceName),
			zap.Int("port", s.cfg.ServicePort),
		)
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("HTTP server error: %w", err)
		}
	}()

	// Self-register controller as healthy host after startup
	// This enables other services (like oauth) to find the controller
	go func() {
		// Wait briefly for HTTP server to be ready
		time.Sleep(100 * time.Millisecond)

		// Check if autoRegisterService is enabled (defaults to true)
		autoRegister := true
		if regData != nil && regData.ServiceConfig != nil {
			autoRegister = regData.ServiceConfig.Awareness.AutoRegisterService
		}

		if autoRegister {
			controllerVersion := "1"
			if regData != nil && regData.Services != nil {
				if ctrlSvc, ok := regData.Services["controller"]; ok && ctrlSvc.Version != "" {
					controllerVersion = ctrlSvc.Version
				}
			}

			hostInfo := types.HostInfo{
				Name:    "controller",
				Version: controllerVersion,
				Host:    "127.0.0.1",
				Port:    s.cfg.ServicePort,
			}

			if err := ctrl.Registry().RegisterHost(hostInfo); err != nil {
				s.logger.Warn("failed to self-register controller host",
					zap.Error(err),
				)
			} else {
				s.logger.Info("controller self-registered as healthy host",
					zap.String("host", hostInfo.Host),
					zap.Int("port", hostInfo.Port),
					zap.String("version", controllerVersion),
				)
			}
		}
	}()

	go func() {
		s.logger.Info("starting maintenance server",
			zap.Int("port", maintPort),
		)
		if err := s.maintServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("maintenance server error: %w", err)
		}
	}()

	// Wait for shutdown signal or error
	select {
	case <-ctx.Done():
		s.logger.Info("shutdown signal received")
		return s.shutdown()
	case err := <-errChan:
		return err
	}
}

// shutdown gracefully shuts down both servers
func (s *Server) shutdown() error {
	ctx, cancel := context.WithTimeout(
		context.Background(),
		time.Duration(s.cfg.Server.ShutdownTimeout)*time.Second,
	)
	defer cancel()

	// Shutdown main server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		s.logger.Error("HTTP server shutdown error", zap.Error(err))
	}

	// Shutdown maintenance server
	if err := s.maintServer.Shutdown(ctx); err != nil {
		s.logger.Error("maintenance server shutdown error", zap.Error(err))
	}

	s.logger.Info("servers stopped")
	return nil
}
