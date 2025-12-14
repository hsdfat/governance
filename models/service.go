package models

import "time"

// Protocol represents the communication protocol type
type Protocol string

const (
	ProtocolHTTP Protocol = "http"
	ProtocolTCP  Protocol = "tcp"
	ProtocolPFCP Protocol = "pfcp"
	ProtocolGTP  Protocol = "gtp"
	ProtocolUDP  Protocol = "udp"
)

// ProviderInfo contains the endpoint information for a service provider
type ProviderInfo struct {
	Protocol Protocol `json:"protocol"`
	IP       string   `json:"ip"`
	Port     int      `json:"port"`
}

// ServiceRegistration represents a service registration request
type ServiceRegistration struct {
	ServiceName      string         `json:"service_name"`
	PodName          string         `json:"pod_name"`
	Providers        []ProviderInfo `json:"providers"`
	HealthCheckURL   string         `json:"health_check_url"`
	NotificationURL  string         `json:"notification_url"`
	Subscriptions    []string       `json:"subscriptions"` // List of service groups to subscribe
}

// ServiceStatus represents the health status of a service
type ServiceStatus string

const (
	StatusHealthy   ServiceStatus = "healthy"
	StatusUnhealthy ServiceStatus = "unhealthy"
	StatusUnknown   ServiceStatus = "unknown"
)

// ServiceInfo represents the internal service information stored in registry
type ServiceInfo struct {
	ServiceName     string
	PodName         string
	Providers       []ProviderInfo
	HealthCheckURL  string
	NotificationURL string
	Subscriptions   []string
	Status          ServiceStatus
	LastHealthCheck time.Time
	RegisteredAt    time.Time
}

// GetKey returns a unique key for the service (service_name:pod_name)
func (s *ServiceInfo) GetKey() string {
	return s.ServiceName + ":" + s.PodName
}
