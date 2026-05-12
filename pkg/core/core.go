package core

import (
	"go.uber.org/zap"
)

// Core provides core functionality
type Core struct {
	logger *zap.Logger
}

// New creates a new Core instance
func New(logger *zap.Logger) *Core {
	return &Core{
		logger: logger,
	}
}

// GetLogger returns a logger for the given service
func (c *Core) GetLogger(serviceName string) *zap.Logger {
	return c.logger.With(zap.String("service", serviceName))
}

// Logger returns the base logger
func (c *Core) Logger() *zap.Logger {
	return c.logger
}
