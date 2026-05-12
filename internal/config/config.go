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

	// MongoDB configuration
	Mongo MongoConfig `mapstructure:"mongo"`

	// Server configuration
	Server ServerConfig `mapstructure:"server"`

	// Maintenance configuration
	Maintenance MaintenanceConfig `mapstructure:"maintenance"`

	// Awareness configuration
	Awareness AwarenessConfig `mapstructure:"awareness"`
}

// MongoConfig holds MongoDB connection settings
type MongoConfig struct {
	URI            string `mapstructure:"uri"`
	Database       string `mapstructure:"database"`
	MaxPoolSize    uint64 `mapstructure:"max_pool_size"`
	MinPoolSize    uint64 `mapstructure:"min_pool_size"`
	ConnectTimeout int    `mapstructure:"connect_timeout"`
}

// ServerConfig holds HTTP server settings
type ServerConfig struct {
	ReadTimeout     int `mapstructure:"read_timeout"`
	WriteTimeout    int `mapstructure:"write_timeout"`
	IdleTimeout     int `mapstructure:"idle_timeout"`
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

	// MongoDB defaults
	v.SetDefault("mongo.uri", "mongodb://localhost:27017")
	v.SetDefault("mongo.database", "core_provision")
	v.SetDefault("mongo.max_pool_size", 100)
	v.SetDefault("mongo.min_pool_size", 10)
	v.SetDefault("mongo.connect_timeout", 5)

	// Server defaults
	v.SetDefault("server.read_timeout", 30)
	v.SetDefault("server.write_timeout", 30)
	v.SetDefault("server.idle_timeout", 60)
	v.SetDefault("server.shutdown_timeout", 10)

	// Maintenance defaults
	v.SetDefault("maintenance.port_increment", 1000)
	v.SetDefault("maintenance.readiness", "/heartbeat")

	// Awareness defaults
	v.SetDefault("awareness.health_check_interval", 5000)
	v.SetDefault("awareness.health_check_timeout", 3000)
	v.SetDefault("awareness.auto_register_host", true)
	v.SetDefault("awareness.mode", "custom")
}
