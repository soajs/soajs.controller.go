package logger

import (
	"os"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Config holds logger configuration
type Config struct {
	Level       string
	ServiceName string
	ServicePort int
	Environment string
	Format      string // "json" or "console"
}

// Logger wraps zap.Logger with SOAJS-specific functionality
type Logger struct {
	*zap.Logger
	config Config
}

// New creates a new SOAJS logger
func New(cfg Config) (*Logger, error) {
	level := parseLevel(cfg.Level)

	encoderConfig := zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     soajsTimeEncoder,
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}

	var encoder zapcore.Encoder
	if cfg.Format == "console" {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		encoder = zapcore.NewConsoleEncoder(encoderConfig)
	} else {
		encoder = zapcore.NewJSONEncoder(encoderConfig)
	}

	core := zapcore.NewCore(
		encoder,
		zapcore.AddSync(os.Stdout),
		level,
	)

	// Add SOAJS fields
	baseFields := []zap.Field{
		zap.String("service", cfg.ServiceName),
		zap.Int("port", cfg.ServicePort),
		zap.String("env", cfg.Environment),
	}

	logger := zap.New(core,
		zap.AddCaller(),
		zap.AddCallerSkip(1),
		zap.Fields(baseFields...),
	)

	return &Logger{
		Logger: logger,
		config: cfg,
	}, nil
}

// WithRequest returns a logger with request-specific fields
func (l *Logger) WithRequest(requestID, method, path, clientIP string) *zap.Logger {
	return l.Logger.With(
		zap.String("request_id", requestID),
		zap.String("method", method),
		zap.String("path", path),
		zap.String("client_ip", clientIP),
	)
}

// WithTenant returns a logger with tenant-specific fields
func (l *Logger) WithTenant(tenantCode, tenantID string) *zap.Logger {
	return l.Logger.With(
		zap.String("tenant_code", tenantCode),
		zap.String("tenant_id", tenantID),
	)
}

// WithService returns a logger with target service fields
func (l *Logger) WithService(serviceName, version string) *zap.Logger {
	return l.Logger.With(
		zap.String("target_service", serviceName),
		zap.String("target_version", version),
	)
}

// LogRequest logs an incoming request
func (l *Logger) LogRequest(requestID, method, path, clientIP string) {
	l.Logger.Info("incoming request",
		zap.String("request_id", requestID),
		zap.String("method", method),
		zap.String("path", path),
		zap.String("client_ip", clientIP),
	)
}

// LogResponse logs a completed response
func (l *Logger) LogResponse(requestID string, statusCode int, duration time.Duration) {
	l.Logger.Info("response",
		zap.String("request_id", requestID),
		zap.Int("status", statusCode),
		zap.Duration("duration", duration),
	)
}

// LogError logs an error with context
func (l *Logger) LogError(requestID, operation string, err error) {
	l.Logger.Error(operation,
		zap.String("request_id", requestID),
		zap.Error(err),
	)
}

// soajsTimeEncoder encodes time in SOAJS format
func soajsTimeEncoder(t time.Time, enc zapcore.PrimitiveArrayEncoder) {
	enc.AppendString(t.Format("2006-01-02T15:04:05.000Z07:00"))
}

func parseLevel(level string) zapcore.Level {
	switch level {
	case "debug":
		return zapcore.DebugLevel
	case "info":
		return zapcore.InfoLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	case "fatal":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

// Sync flushes any buffered log entries
func (l *Logger) Sync() error {
	return l.Logger.Sync()
}
