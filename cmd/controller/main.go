package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/soajs/soajs.controller.go/internal/config"
	"github.com/soajs/soajs.controller.go/internal/server"
	"github.com/soajs/soajs.controller.go/pkg/logger"
	"go.uber.org/zap"
)

func main() {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		panic("failed to load configuration: " + err.Error())
	}

	// Initialize SOAJS logger
	logCfg := logger.Config{
		Level:       cfg.LogLevel,
		ServiceName: cfg.ServiceName,
		ServicePort: cfg.ServicePort,
		Environment: cfg.Environment,
		Format:      getLogFormat(),
	}

	log, err := logger.New(logCfg)
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
	defer log.Sync()

	log.Info("starting SOAJS controller",
		zap.String("version", cfg.ServiceVersion),
	)

	// Create server
	srv := server.New(cfg, log.Logger)

	// Setup graceful shutdown
	ctx, cancel := signal.NotifyContext(
		context.Background(),
		syscall.SIGINT,
		syscall.SIGTERM,
	)
	defer cancel()

	// Run server
	if err := srv.Run(ctx); err != nil {
		log.Error("server error", zap.Error(err))
		os.Exit(1)
	}
}

// getLogFormat returns log format based on environment
func getLogFormat() string {
	if os.Getenv("SOAJS_LOG_FORMAT") == "console" {
		return "console"
	}
	// Default to JSON for production
	if os.Getenv("SOAJS_ENV") == "dev" || os.Getenv("SOAJS_ENV") == "development" {
		return "console"
	}
	return "json"
}
