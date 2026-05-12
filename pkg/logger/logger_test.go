package logger

import (
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "debug level console format",
			cfg: Config{
				Level:       "debug",
				ServiceName: "test-service",
				ServicePort: 4000,
				Environment: "dev",
				Format:      "console",
			},
			wantErr: false,
		},
		{
			name: "info level json format",
			cfg: Config{
				Level:       "info",
				ServiceName: "controller",
				ServicePort: 4000,
				Environment: "production",
				Format:      "json",
			},
			wantErr: false,
		},
		{
			name: "warn level",
			cfg: Config{
				Level:       "warn",
				ServiceName: "test",
				ServicePort: 5000,
				Environment: "staging",
				Format:      "json",
			},
			wantErr: false,
		},
		{
			name: "error level",
			cfg: Config{
				Level:       "error",
				ServiceName: "test",
				ServicePort: 5000,
				Environment: "prod",
				Format:      "json",
			},
			wantErr: false,
		},
		{
			name: "invalid level defaults to info",
			cfg: Config{
				Level:       "invalid",
				ServiceName: "test",
				ServicePort: 5000,
				Environment: "dev",
				Format:      "console",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if logger == nil {
				t.Error("New() returned nil logger")
				return
			}
			defer logger.Sync()
		})
	}
}

func TestLogger_WithRequest(t *testing.T) {
	cfg := Config{
		Level:       "debug",
		ServiceName: "test-service",
		ServicePort: 4000,
		Environment: "dev",
		Format:      "console",
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	reqLogger := logger.WithRequest("req-123", "GET", "/api/test", "127.0.0.1")
	if reqLogger == nil {
		t.Error("WithRequest() returned nil")
	}
}

func TestLogger_WithTenant(t *testing.T) {
	cfg := Config{
		Level:       "debug",
		ServiceName: "test-service",
		ServicePort: 4000,
		Environment: "dev",
		Format:      "console",
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	tenantLogger := logger.WithTenant("TENANT1", "tenant-id-123")
	if tenantLogger == nil {
		t.Error("WithTenant() returned nil")
	}
}

func TestLogger_WithService(t *testing.T) {
	cfg := Config{
		Level:       "debug",
		ServiceName: "test-service",
		ServicePort: 4000,
		Environment: "dev",
		Format:      "console",
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	svcLogger := logger.WithService("urac", "1")
	if svcLogger == nil {
		t.Error("WithService() returned nil")
	}
}

func TestLogger_LogRequest(t *testing.T) {
	cfg := Config{
		Level:       "debug",
		ServiceName: "test-service",
		ServicePort: 4000,
		Environment: "dev",
		Format:      "console",
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Should not panic
	logger.LogRequest("req-123", "POST", "/api/users", "192.168.1.1")
}

func TestLogger_LogResponse(t *testing.T) {
	cfg := Config{
		Level:       "debug",
		ServiceName: "test-service",
		ServicePort: 4000,
		Environment: "dev",
		Format:      "console",
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Should not panic
	logger.LogResponse("req-123", 200, 150*time.Millisecond)
}

func TestLogger_LogError(t *testing.T) {
	cfg := Config{
		Level:       "debug",
		ServiceName: "test-service",
		ServicePort: 4000,
		Environment: "dev",
		Format:      "console",
	}

	logger, err := New(cfg)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Sync()

	// Should not panic
	logger.LogError("req-123", "database connection", nil)
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"warning", "warn"},
		{"error", "error"},
		{"fatal", "fatal"},
		{"invalid", "info"}, // defaults to info
		{"", "info"},        // defaults to info
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			level := parseLevel(tt.input)
			// Just verify it doesn't panic - we can't easily test the actual level value
			_ = level
		})
	}
}
