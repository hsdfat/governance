package models

import "time"

// ManagerConfig contains configuration for the governance manager
type ManagerConfig struct {
	// Manager HTTP server settings
	ServerPort int `json:"server_port"`

	// Health check settings
	HealthCheckInterval time.Duration `json:"health_check_interval"` // How often to check health
	HealthCheckTimeout  time.Duration `json:"health_check_timeout"`  // Timeout for health check HTTP call
	HealthCheckRetry    int           `json:"health_check_retry"`    // Number of retries before marking unhealthy

	// Notification settings
	NotificationInterval time.Duration `json:"notification_interval"` // Periodic reconcile interval
	NotificationTimeout  time.Duration `json:"notification_timeout"`  // Timeout for notification HTTP call

	// Event queue settings
	EventQueueSize int `json:"event_queue_size"` // Event queue buffer size
}

// DefaultConfig returns a default configuration
func DefaultConfig() *ManagerConfig {
	return &ManagerConfig{
		ServerPort:           8080,
		HealthCheckInterval:  30 * time.Second,
		HealthCheckTimeout:   5 * time.Second,
		HealthCheckRetry:     3,
		NotificationInterval: 60 * time.Second,
		NotificationTimeout:  5 * time.Second,
		EventQueueSize:       1000,
	}
}
