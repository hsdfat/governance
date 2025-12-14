package registry

import (
	"time"

	"github.com/chronnie/governance/models"
)

// Registry manages all registered services in memory
// No locks needed because it's accessed only by single event queue worker
type Registry struct {
	// services maps service key (service_name:pod_name) to ServiceInfo
	services map[string]*models.ServiceInfo

	// subscriptions maps service_name to list of subscriber keys who subscribed to it
	// When service_name changes, notify all subscribers
	subscriptions map[string][]string
}

// NewRegistry creates a new empty registry
func NewRegistry() *Registry {
	return &Registry{
		services:      make(map[string]*models.ServiceInfo),
		subscriptions: make(map[string][]string),
	}
}

// Register adds or updates a service in the registry
func (r *Registry) Register(reg *models.ServiceRegistration) *models.ServiceInfo {
	serviceInfo := &models.ServiceInfo{
		ServiceName:     reg.ServiceName,
		PodName:         reg.PodName,
		Providers:       reg.Providers,
		HealthCheckURL:  reg.HealthCheckURL,
		NotificationURL: reg.NotificationURL,
		Subscriptions:   reg.Subscriptions,
		Status:          models.StatusUnknown, // Initial status is unknown
		RegisteredAt:    time.Now(),
		LastHealthCheck: time.Time{},
	}

	key := serviceInfo.GetKey()

	// Remove old subscriptions if service already exists
	if oldService, exists := r.services[key]; exists {
		r.removeSubscriptions(key, oldService.Subscriptions)
	}

	// Add service to registry
	r.services[key] = serviceInfo

	// Add new subscriptions
	r.addSubscriptions(key, reg.Subscriptions)

	return serviceInfo
}

// Unregister removes a service from the registry
func (r *Registry) Unregister(serviceName, podName string) *models.ServiceInfo {
	key := serviceName + ":" + podName

	service, exists := r.services[key]
	if !exists {
		return nil
	}

	// Remove subscriptions
	r.removeSubscriptions(key, service.Subscriptions)

	// Remove from registry
	delete(r.services, key)

	return service
}

// Get retrieves a service by key
func (r *Registry) Get(key string) (*models.ServiceInfo, bool) {
	service, exists := r.services[key]
	return service, exists
}

// GetByServiceName returns all pods of a service
func (r *Registry) GetByServiceName(serviceName string) []*models.ServiceInfo {
	var result []*models.ServiceInfo
	for _, service := range r.services {
		if service.ServiceName == serviceName {
			result = append(result, service)
		}
	}
	return result
}

// GetAllServices returns all registered services
func (r *Registry) GetAllServices() []*models.ServiceInfo {
	result := make([]*models.ServiceInfo, 0, len(r.services))
	for _, service := range r.services {
		result = append(result, service)
	}
	return result
}

// UpdateHealthStatus updates the health status of a service
func (r *Registry) UpdateHealthStatus(key string, status models.ServiceStatus) bool {
	service, exists := r.services[key]
	if !exists {
		return false
	}

	oldStatus := service.Status
	service.Status = status
	service.LastHealthCheck = time.Now()

	// Return true if status changed
	return oldStatus != status
}

// GetSubscribers returns all subscriber keys for a given service name
func (r *Registry) GetSubscribers(serviceName string) []string {
	subscribers, exists := r.subscriptions[serviceName]
	if !exists {
		return []string{}
	}
	// Return a copy to prevent external modification
	result := make([]string, len(subscribers))
	copy(result, subscribers)
	return result
}

// GetSubscriberServices returns all ServiceInfo of subscribers for a given service name
func (r *Registry) GetSubscriberServices(serviceName string) []*models.ServiceInfo {
	subscriberKeys := r.GetSubscribers(serviceName)
	result := make([]*models.ServiceInfo, 0, len(subscriberKeys))

	for _, key := range subscriberKeys {
		if service, exists := r.services[key]; exists {
			result = append(result, service)
		}
	}

	return result
}

// addSubscriptions adds subscriptions for a service
func (r *Registry) addSubscriptions(subscriberKey string, subscriptions []string) {
	for _, serviceName := range subscriptions {
		if r.subscriptions[serviceName] == nil {
			r.subscriptions[serviceName] = []string{}
		}
		// Add subscriber if not already present
		if !r.containsSubscriber(serviceName, subscriberKey) {
			r.subscriptions[serviceName] = append(r.subscriptions[serviceName], subscriberKey)
		}
	}
}

// removeSubscriptions removes subscriptions for a service
func (r *Registry) removeSubscriptions(subscriberKey string, subscriptions []string) {
	for _, serviceName := range subscriptions {
		r.subscriptions[serviceName] = r.removeFromSlice(r.subscriptions[serviceName], subscriberKey)
		// Clean up empty subscription lists
		if len(r.subscriptions[serviceName]) == 0 {
			delete(r.subscriptions, serviceName)
		}
	}
}

// containsSubscriber checks if a subscriber is already in the list
func (r *Registry) containsSubscriber(serviceName, subscriberKey string) bool {
	subscribers := r.subscriptions[serviceName]
	for _, sub := range subscribers {
		if sub == subscriberKey {
			return true
		}
	}
	return false
}

// removeFromSlice removes an item from a slice
func (r *Registry) removeFromSlice(slice []string, item string) []string {
	result := []string{}
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}
