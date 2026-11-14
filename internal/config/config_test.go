package config

import (
	"os"
	"reflect"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	// Clear any env vars that might interfere
	os.Unsetenv("SOAJS_SERVICE_PORT")
	os.Unsetenv("SOAJS_SERVICE_NAME")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check defaults
	if cfg.ServiceName != "controller" {
		t.Errorf("ServiceName = %q, want %q", cfg.ServiceName, "controller")
	}
	if cfg.ServicePort != 4000 {
		t.Errorf("ServicePort = %d, want %d", cfg.ServicePort, 4000)
	}
	if cfg.Environment != "dev" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "dev")
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Set environment variables
	os.Setenv("SOAJS_SERVICE_PORT", "5000")
	os.Setenv("SOAJS_ENVIRONMENT", "production")
	defer func() {
		os.Unsetenv("SOAJS_SERVICE_PORT")
		os.Unsetenv("SOAJS_ENVIRONMENT")
	}()

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ServicePort != 5000 {
		t.Errorf("ServicePort = %d, want %d", cfg.ServicePort, 5000)
	}
	if cfg.Environment != "production" {
		t.Errorf("Environment = %q, want %q", cfg.Environment, "production")
	}
}

func TestLoadFromEnv_SOAJS_ENV(t *testing.T) {
	// Test Node.js compatibility: SOAJS_ENV should work
	os.Setenv("SOAJS_ENV", "dashboard")
	defer os.Unsetenv("SOAJS_ENV")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Environment != "dashboard" {
		t.Errorf("Environment = %q, want %q (SOAJS_ENV compatibility)", cfg.Environment, "dashboard")
	}
}

// configs/config.example.yaml, used as config.yaml, gives the defaults: every key it shows is one
// the gateway reads, with its default value.
func TestExampleConfigIsTheDefaults(t *testing.T) {
	raw, err := os.ReadFile("../../configs/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/config.yaml", raw, 0o600); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("the example does not load: %v", err)
	}
	if !reflect.DeepEqual(cfg, defaults) {
		t.Errorf("the example is not the defaults:\n got  %+v\n want %+v", cfg, defaults)
	}
}
