package registry

import (
	"context"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
)

// failureTracker tracks consecutive health check failures in-memory
type failureTracker struct {
	consecutiveFailures int
	firstFailureAt      time.Time
}

// Registry manages all registered services using a pluggable storage backend
// No locks needed because it's accessed only by single event queue worker
type Registry struct {
	store           storage.RegistryStore
	logger          logger.Logger
	ctx             context.Context
	failureTracking map[string]*failureTracker // In-memory failure tracking (service_key -> failures)
}

// NewRegistry creates a new registry with the given storage backend
func NewRegistry(store storage.RegistryStore, log logger.Logger) *Registry {
	return &Registry{
		store:           store,
		logger:          log,
		ctx:             context.Background(),
		failureTracking: make(map[string]*failureTracker),
	}
}

// Register adds or updates a service in the registry
func (r *Registry) Register(reg *models.ServiceRegistration) *models.ServiceInfo {
	r.logger.Debugw("Registry: Register called",
		"service_name", reg.ServiceName,
		"pod_name", reg.PodName,
		"subscriptions_count", len(reg.Subscriptions),
	)

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
	if oldService, err := r.store.GetService(r.ctx, key); err == nil {
		r.logger.Debugw("Registry: Service already exists, removing old subscriptions",
			"service_key", key,
			"old_subscriptions_count", len(oldService.Subscriptions),
		)
		r.removeSubscriptions(key, oldService.Subscriptions)
	} else {
		r.logger.Debugw("Registry: New service registration",
			"service_key", key,
		)
	}

	// Save service to storage
	if err := r.store.SaveService(r.ctx, serviceInfo); err != nil {
		r.logger.Errorw("Registry: Failed to save service to storage",
			"service_key", key,
			"error", err,
		)
		return serviceInfo
	}

	r.logger.Debugw("Registry: Service saved to storage",
		"service_key", key,
	)

	// Add new subscriptions
	if len(reg.Subscriptions) > 0 {
		// Convert subscriptions to service names for logging
		subNames := make([]string, len(reg.Subscriptions))
		for i, sub := range reg.Subscriptions {
			subNames[i] = sub.ServiceName
		}
		r.logger.Debugw("Registry: Adding subscriptions",
			"service_key", key,
			"subscriptions", subNames,
		)
		r.addSubscriptions(key, reg.Subscriptions)
	}

	r.logger.Infow("Registry: Service registered successfully",
		"service_key", key,
		"subscriptions_count", len(reg.Subscriptions),
	)

	return serviceInfo
}

// Unregister removes a service from the registry
func (r *Registry) Unregister(serviceName, podName string) *models.ServiceInfo {
	key := serviceName + ":" + podName

	r.logger.Debugw("Registry: Unregister called",
		"service_key", key,
	)

	service, err := r.store.GetService(r.ctx, key)
	if err != nil {
		r.logger.Warnw("Registry: Service not found for unregistration",
			"service_key", key,
			"error", err,
		)
		return nil
	}

	// Remove subscriptions
	if len(service.Subscriptions) > 0 {
		r.logger.Debugw("Registry: Removing subscriptions",
			"service_key", key,
			"subscriptions_count", len(service.Subscriptions),
		)
		r.removeSubscriptions(key, service.Subscriptions)
	}

	// Remove from storage
	if err := r.store.DeleteService(r.ctx, key); err != nil {
		r.logger.Errorw("Registry: Failed to delete service from storage",
			"service_key", key,
			"error", err,
		)
	} else {
		r.logger.Debugw("Registry: Service deleted from storage",
			"service_key", key,
		)
	}

	// Clean up in-memory failure tracking
	delete(r.failureTracking, key)

	r.logger.Infow("Registry: Service unregistered successfully",
		"service_key", key,
	)

	return service
}

// Get retrieves a service by key
func (r *Registry) Get(key string) (*models.ServiceInfo, bool) {
	service, err := r.store.GetService(r.ctx, key)
	if err != nil {
		return nil, false
	}
	return service, true
}

// GetByServiceName returns all pods of a service
func (r *Registry) GetByServiceName(serviceName string) []*models.ServiceInfo {
	result, err := r.store.GetServicesByName(r.ctx, serviceName)
	if err != nil {
		return []*models.ServiceInfo{}
	}
	return result
}

// GetAllServices returns all registered services
func (r *Registry) GetAllServices() []*models.ServiceInfo {
	result, err := r.store.GetAllServices(r.ctx)
	if err != nil {
		return []*models.ServiceInfo{}
	}
	return result
}

// UpdateHealthStatus updates the health status of a service and tracks consecutive failures
func (r *Registry) UpdateHealthStatus(key string, status models.ServiceStatus) bool {
	r.logger.Debugw("Registry: UpdateHealthStatus called",
		"service_key", key,
		"new_status", string(status),
	)

	service, err := r.store.GetService(r.ctx, key)
	if err != nil {
		r.logger.Warnw("Registry: Service not found for health status update",
			"service_key", key,
			"error", err,
		)
		return false
	}

	oldStatus := service.Status
	timestamp := time.Now()

	// Track consecutive failures
	switch status {
	case models.StatusUnhealthy:
		// If this is a new failure (was healthy before), reset the failure tracking
		if oldStatus != models.StatusUnhealthy {
			service.ConsecutiveFailures = 1
			service.FirstFailureAt = timestamp
		} else {
			// Increment consecutive failures
			service.ConsecutiveFailures++
			// Set FirstFailureAt if it's not already set (e.g., service loaded from DB)
			if service.FirstFailureAt.IsZero() {
				service.FirstFailureAt = timestamp
			}
		}

		r.logger.Warnw("Registry: Health check failed",
			"service_key", key,
			"consecutive_failures", service.ConsecutiveFailures,
			"first_failure_at", service.FirstFailureAt,
		)
	case models.StatusHealthy:
		// Reset failure tracking when service becomes healthy
		if service.ConsecutiveFailures > 0 {
			r.logger.Infow("Registry: Service recovered",
				"service_key", key,
				"previous_failures", service.ConsecutiveFailures,
			)
			service.ConsecutiveFailures = 0
			service.FirstFailureAt = time.Time{}
		}
	}

	// Update status and timestamp
	service.Status = status
	service.LastHealthCheck = timestamp

	// Save the entire service object to persist failure tracking
	if err := r.store.SaveService(r.ctx, service); err != nil {
		r.logger.Errorw("Registry: Failed to update service in storage",
			"service_key", key,
			"old_status", string(oldStatus),
			"new_status", string(status),
			"error", err,
		)
		return false
	}

	statusChanged := oldStatus != status
	if statusChanged {
		r.logger.Infow("Registry: Health status updated",
			"service_key", key,
			"old_status", string(oldStatus),
			"new_status", string(status),
		)
	} else {
		r.logger.Debugw("Registry: Health status unchanged",
			"service_key", key,
			"status", string(status),
		)
	}

	return statusChanged
}

// GetConsecutiveFailures returns the number of consecutive failures for a service
func (r *Registry) GetConsecutiveFailures(key string) int {
	tracker, exists := r.failureTracking[key]
	if !exists {
		return 0
	}
	return tracker.consecutiveFailures
}

// TrackHealthCheckFailure increments the failure counter and returns the new count
func (r *Registry) TrackHealthCheckFailure(key string) int {
	tracker, exists := r.failureTracking[key]
	if !exists {
		// First failure
		tracker = &failureTracker{
			consecutiveFailures: 1,
			firstFailureAt:      time.Now(),
		}
		r.failureTracking[key] = tracker
	} else {
		// Increment consecutive failures
		tracker.consecutiveFailures++
	}

	r.logger.Debugw("Registry: Tracked health check failure",
		"service_key", key,
		"consecutive_failures", tracker.consecutiveFailures,
		"first_failure_at", tracker.firstFailureAt,
	)

	return tracker.consecutiveFailures
}

// ResetHealthCheckFailures resets the failure counter for a service
func (r *Registry) ResetHealthCheckFailures(key string) {
	_, exists := r.failureTracking[key]
	if exists {
		r.logger.Debugw("Registry: Resetting health check failures",
			"service_key", key,
		)
		delete(r.failureTracking, key)
	}
}

// GetSubscribers returns all subscriber keys for a given service name
func (r *Registry) GetSubscribers(serviceName string) []string {
	subscribers, err := r.store.GetSubscribers(r.ctx, serviceName)
	if err != nil {
		return []string{}
	}
	return subscribers
}

// GetSubscriberServices returns all ServiceInfo of subscribers for a given service name
func (r *Registry) GetSubscriberServices(serviceName string) []*models.ServiceInfo {
	result, err := r.store.GetSubscriberServices(r.ctx, serviceName)
	if err != nil {
		return []*models.ServiceInfo{}
	}
	return result
}

// addSubscriptions adds subscriptions for a service
func (r *Registry) addSubscriptions(subscriberKey string, subscriptions []models.Subscription) {
	for _, sub := range subscriptions {
		if err := r.store.AddSubscription(r.ctx, subscriberKey, sub.ServiceName); err != nil {
			r.logger.Errorw("Registry: Failed to add subscription",
				"subscriber_key", subscriberKey,
				"service_name", sub.ServiceName,
				"error", err,
			)
		} else {
			r.logger.Debugw("Registry: Subscription added",
				"subscriber_key", subscriberKey,
				"service_name", sub.ServiceName,
			)
		}
	}
}

// removeSubscriptions removes subscriptions for a service
func (r *Registry) removeSubscriptions(subscriberKey string, subscriptions []models.Subscription) {
	for _, sub := range subscriptions {
		if err := r.store.RemoveSubscription(r.ctx, subscriberKey, sub.ServiceName); err != nil {
			r.logger.Errorw("Registry: Failed to remove subscription",
				"subscriber_key", subscriberKey,
				"service_name", sub.ServiceName,
				"error", err,
			)
		} else {
			r.logger.Debugw("Registry: Subscription removed",
				"subscriber_key", subscriberKey,
				"service_name", sub.ServiceName,
			)
		}
	}
}
