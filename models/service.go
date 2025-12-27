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

// ProviderID represents common provider identifiers
// Services can use these predefined constants or define custom provider IDs
type ProviderID string

const (
	// EIR service providers
	ProviderEIRDiameter ProviderID = "eir-diameter"
	ProviderEIRHTTP     ProviderID = "eir-http"

	// SMF service providers
	ProviderSMFPFCP ProviderID = "smf-pfcp"
	ProviderSMFHTTP ProviderID = "smf-http"

	// UPF service providers
	ProviderUPFPFCP ProviderID = "upf-pfcp"
	ProviderUPFGTP  ProviderID = "upf-gtp"

	// AMF service providers
	ProviderAMFNGAP ProviderID = "amf-ngap"
	ProviderAMFHTTP ProviderID = "amf-http"

	// Generic providers (can be used by any service)
	ProviderHTTP     ProviderID = "http"
	ProviderDiameter ProviderID = "diameter"
	ProviderRadius   ProviderID = "radius"
)

// ProviderInfo contains the endpoint information for a service provider
type ProviderInfo struct {
	ProviderID string   `json:"provider_id"` // Globally unique identifier (e.g., "eir-diameter", "smf-pfcp")
	Protocol   Protocol `json:"protocol"`
	IP         string   `json:"ip"`
	Port       int      `json:"port"`
}

// Subscription represents a subscription to a service with optional provider filtering
type Subscription struct {
	ServiceName string   `json:"service_name"`            // Service to subscribe to
	ProviderIDs []string `json:"provider_ids,omitempty"`  // Optional: specific provider IDs to subscribe to (empty = all providers)
}

// ServiceRegistration represents a service registration request
type ServiceRegistration struct {
	ServiceName      string         `json:"service_name"`
	PodName          string         `json:"pod_name"`
	Providers        []ProviderInfo `json:"providers"`
	HealthCheckURL   string         `json:"health_check_url"`
	NotificationURL  string         `json:"notification_url"`
	Subscriptions    []Subscription `json:"subscriptions"` // List of service subscriptions with optional provider filtering
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
	ServiceName            string
	PodName                string
	Providers              []ProviderInfo
	HealthCheckURL         string
	NotificationURL        string
	Subscriptions          []Subscription
	Status                 ServiceStatus
	LastHealthCheck        time.Time
	RegisteredAt           time.Time
	ConsecutiveFailures    int       // Number of consecutive health check failures
	FirstFailureAt         time.Time // Timestamp of first failure in current streak
}

// GetKey returns a unique key for the service (service_name:pod_name)
func (s *ServiceInfo) GetKey() string {
	return s.ServiceName + ":" + s.PodName
}

// RegistrationResponse represents the response returned when a service registers
type RegistrationResponse struct {
	Status             string                    `json:"status"`
	Message            string                    `json:"message"`
	ServiceName        string                    `json:"service_name"`
	Pods               []PodInfo                 `json:"pods"`                 // Pods of the registered service
	SubscribedServices map[string][]PodInfo      `json:"subscribed_services"`  // Pods of services this client subscribed to
}

// ErrorResponse represents an error response from the manager
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
