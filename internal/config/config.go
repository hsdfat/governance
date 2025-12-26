package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config holds the application configuration
type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Manager  ManagerConfig
	Logging  LoggingConfig
}

// ServerConfig holds HTTP server configuration
type ServerConfig struct {
	Port int
}

// DatabaseConfig holds PostgreSQL configuration
type DatabaseConfig struct {
	Host            string
	Port            int
	Database        string
	Username        string
	Password        string
	SSLMode         string
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// ManagerConfig holds governance manager configuration
type ManagerConfig struct {
	HealthCheckInterval  time.Duration
	HealthCheckTimeout   time.Duration
	HealthCheckRetry     int
	NotificationInterval time.Duration
	NotificationTimeout  time.Duration
	EventQueueSize       int
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	Level  string // "debug", "info", "warn", "error"
	Format string // "json", "text"
}

// Load loads configuration from file and environment variables
// Priority order (highest to lowest):
// 1. Environment variables (prefixed with GOV_)
// 2. Config file specified by configPath
// 3. config.yaml in standard paths
// 4. config.default.yaml as fallback
// 5. Hardcoded defaults
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Set default values (lowest priority)
	setDefaults(v)

	// Set config file paths
	if configPath != "" {
		// Use specified config file
		v.SetConfigFile(configPath)
	} else {
		// Search for config.yaml first, then fall back to config.default.yaml
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath(".")
		v.AddConfigPath("./config")
		v.AddConfigPath("/etc/governance")
	}

	// Read environment variables (highest priority)
	v.AutomaticEnv()
	v.SetEnvPrefix("GOV")

	// Try to read config file
	configFileRead := false
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			// Config file not found, try default config
			v.SetConfigName("config.default")
			if err := v.ReadInConfig(); err != nil {
				if _, ok := err.(viper.ConfigFileNotFoundError); ok {
					// No config files found; using defaults and environment variables
					fmt.Println("Warning: No config file found, using defaults and environment variables")
				} else {
					return nil, fmt.Errorf("failed to read default config file: %w", err)
				}
			} else {
				configFileRead = true
			}
		} else {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	} else {
		configFileRead = true
	}

	if configFileRead {
		fmt.Printf("Using config file: %s\n", v.ConfigFileUsed())
	}

	// Unmarshal config
	var config Config
	if err := v.Unmarshal(&config); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	// Validate config
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return &config, nil
}

// setDefaults sets default configuration values
func setDefaults(v *viper.Viper) {
	// Server defaults
	v.SetDefault("server.port", 8080)

	// Database defaults
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.database", "governance_db")
	v.SetDefault("database.username", "postgres")
	v.SetDefault("database.password", "postgres")
	v.SetDefault("database.sslMode", "disable")
	v.SetDefault("database.maxOpenConns", 25)
	v.SetDefault("database.maxIdleConns", 5)
	v.SetDefault("database.connMaxLifetime", "5m")

	// Manager defaults
	v.SetDefault("manager.healthCheckInterval", "30s")
	v.SetDefault("manager.healthCheckTimeout", "5s")
	v.SetDefault("manager.healthCheckRetry", 3)
	v.SetDefault("manager.notificationInterval", "60s")
	v.SetDefault("manager.notificationTimeout", "5s")
	v.SetDefault("manager.eventQueueSize", 1000)

	// Logging defaults
	v.SetDefault("logging.level", "info")
	v.SetDefault("logging.format", "text")
}

// Validate validates the configuration
func (c *Config) Validate() error {
	// Validate Server configuration
	if err := c.Server.Validate(); err != nil {
		return fmt.Errorf("server config: %w", err)
	}

	// Validate Database configuration
	if err := c.Database.Validate(); err != nil {
		return fmt.Errorf("database config: %w", err)
	}

	// Validate Manager configuration
	if err := c.Manager.Validate(); err != nil {
		return fmt.Errorf("manager config: %w", err)
	}

	// Validate Logging configuration
	if err := c.Logging.Validate(); err != nil {
		return fmt.Errorf("logging config: %w", err)
	}

	return nil
}

// Validate validates the ServerConfig
func (c *ServerConfig) Validate() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}
	return nil
}

// Validate validates the DatabaseConfig
func (c *DatabaseConfig) Validate() error {
	if c.Host == "" {
		return fmt.Errorf("host is required")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535, got %d", c.Port)
	}
	if c.Database == "" {
		return fmt.Errorf("database is required")
	}
	if c.Username == "" {
		return fmt.Errorf("username is required")
	}
	validSSLModes := map[string]bool{
		"disable":     true,
		"require":     true,
		"verify-ca":   true,
		"verify-full": true,
	}
	if !validSSLModes[c.SSLMode] {
		return fmt.Errorf("sslMode must be one of: disable, require, verify-ca, verify-full")
	}
	if c.MaxOpenConns < 1 {
		return fmt.Errorf("maxOpenConns must be at least 1")
	}
	if c.MaxIdleConns < 0 {
		return fmt.Errorf("maxIdleConns must be non-negative")
	}
	if c.MaxIdleConns > c.MaxOpenConns {
		return fmt.Errorf("maxIdleConns (%d) cannot exceed maxOpenConns (%d)", c.MaxIdleConns, c.MaxOpenConns)
	}
	if c.ConnMaxLifetime < 0 {
		return fmt.Errorf("connMaxLifetime must be non-negative")
	}
	return nil
}

// Validate validates the ManagerConfig
func (c *ManagerConfig) Validate() error {
	if c.HealthCheckInterval < 0 {
		return fmt.Errorf("healthCheckInterval must be non-negative")
	}
	if c.HealthCheckTimeout < 0 {
		return fmt.Errorf("healthCheckTimeout must be non-negative")
	}
	if c.HealthCheckRetry < 1 {
		return fmt.Errorf("healthCheckRetry must be at least 1")
	}
	if c.NotificationInterval < 0 {
		return fmt.Errorf("notificationInterval must be non-negative")
	}
	if c.NotificationTimeout < 0 {
		return fmt.Errorf("notificationTimeout must be non-negative")
	}
	if c.EventQueueSize < 1 {
		return fmt.Errorf("eventQueueSize must be at least 1")
	}
	return nil
}

// Validate validates the LoggingConfig
func (c *LoggingConfig) Validate() error {
	validLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLevels[c.Level] {
		return fmt.Errorf("level must be one of: debug, info, warn, error")
	}
	validFormats := map[string]bool{
		"json": true,
		"text": true,
	}
	if !validFormats[c.Format] {
		return fmt.Errorf("format must be one of: json, text")
	}
	return nil
}
