package config

import (
	"strings"

	"github.com/spf13/viper"
)

// Config holds all configuration for the controller
type Config struct {
	// Service configuration
	ServiceName    string `mapstructure:"service_name"`
	ServiceVersion string `mapstructure:"service_version"`
	ServiceGroup   string `mapstructure:"service_group"`
	ServicePort    int    `mapstructure:"service_port"`

	// Environment
	Environment string `mapstructure:"environment"`
	LogLevel    string `mapstructure:"log_level"`

	// Profile is SOAJS_PROFILE: the file that says where the provision database lives
	Profile string `mapstructure:"profile"`

	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Maintenance configuration
	Maintenance MaintenanceConfig `mapstructure:"maintenance"`

	// Awareness configuration
	Awareness AwarenessConfig `mapstructure:"awareness"`
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	ShutdownTimeout int `mapstructure:"shutdown_timeout"`
}

// MaintenanceConfig holds maintenance server settings
type MaintenanceConfig struct {
	PortIncrement int      `mapstructure:"port_increment"`
	Readiness     string   `mapstructure:"readiness"`
	Commands      []string `mapstructure:"commands"`
}

// AwarenessConfig holds service discovery settings
type AwarenessConfig struct {
	HealthCheckInterval int    `mapstructure:"health_check_interval"`
	HealthCheckTimeout  int    `mapstructure:"health_check_timeout"`
	AutoRegisterHost    bool   `mapstructure:"auto_register_host"`
	Mode                string `mapstructure:"mode"` // "custom" or "ha"
}

// Load loads configuration from environment and files
func Load() (*Config, error) {
	v := viper.New()

	// Set defaults
	setDefaults(v)

	// Read from environment variables
	v.SetEnvPrefix("SOAJS")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Bind SOAJS_ENV to environment (Node.js compatibility)
	// Node.js uses SOAJS_ENV, viper would expect SOAJS_ENVIRONMENT
	v.BindEnv("environment", "SOAJS_ENV")
	v.BindEnv("profile", "SOAJS_PROFILE")

	// Read from config file if exists
	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./configs")
	v.AddConfigPath("/etc/soajs/")

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, err
		}
		// Config file not found is OK, we use defaults and env vars
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	// Service defaults
	v.SetDefault("service_name", "controller")
	v.SetDefault("service_version", "1")
	v.SetDefault("service_group", "Gateway")
	v.SetDefault("service_port", 4000)

	// Environment defaults
	v.SetDefault("environment", "dev")
	v.SetDefault("log_level", "info")

	// Server defaults
	v.SetDefault("server.shutdown_timeout", 10)

	// Maintenance defaults (Node.js uses maintenanceInc = 1001)
	v.SetDefault("maintenance.port_increment", 1001)
	v.SetDefault("maintenance.readiness", "/heartbeat")

	// Awareness defaults
	v.SetDefault("awareness.health_check_interval", 5000)
	v.SetDefault("awareness.health_check_timeout", 3000)
	v.SetDefault("awareness.auto_register_host", true)
	v.SetDefault("awareness.mode", "custom")
}
