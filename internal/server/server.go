package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/soajs/soajs.controller.go/internal/config"
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

	// Create maintenance server
	maintPort := s.cfg.ServicePort + s.cfg.Maintenance.PortIncrement
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
