package registry

import (
	"context"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
	"go.uber.org/zap"
)

// Registry manages all registered services using a pluggable storage backend
// No locks needed because it's accessed only by single event queue worker
type Registry struct {
	store storage.RegistryStore
	ctx   context.Context
}

// NewRegistry creates a new registry with the given storage backend
func NewRegistry(store storage.RegistryStore) *Registry {
	return &Registry{
		store: store,
		ctx:   context.Background(),
	}
}

// Register adds or updates a service in the registry
func (r *Registry) Register(reg *models.ServiceRegistration) *models.ServiceInfo {
	logger.Debug("Registry: Register called",
		zap.String("service_name", reg.ServiceName),
		zap.String("pod_name", reg.PodName),
		zap.Int("subscriptions_count", len(reg.Subscriptions)),
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
		logger.Debug("Registry: Service already exists, removing old subscriptions",
			zap.String("service_key", key),
			zap.Int("old_subscriptions_count", len(oldService.Subscriptions)),
		)
		r.removeSubscriptions(key, oldService.Subscriptions)
	} else {
		logger.Debug("Registry: New service registration",
			zap.String("service_key", key),
		)
	}

	// Save service to storage
	if err := r.store.SaveService(r.ctx, serviceInfo); err != nil {
		logger.Error("Registry: Failed to save service to storage",
			zap.String("service_key", key),
			zap.Error(err),
		)
		return serviceInfo
	}

	logger.Debug("Registry: Service saved to storage",
		zap.String("service_key", key),
	)

	// Add new subscriptions
	if len(reg.Subscriptions) > 0 {
		// Convert subscriptions to service names for logging
		subNames := make([]string, len(reg.Subscriptions))
		for i, sub := range reg.Subscriptions {
			subNames[i] = sub.ServiceName
		}
		logger.Debug("Registry: Adding subscriptions",
			zap.String("service_key", key),
			zap.Strings("subscriptions", subNames),
		)
		r.addSubscriptions(key, reg.Subscriptions)
	}

	logger.Info("Registry: Service registered successfully",
		zap.String("service_key", key),
		zap.Int("subscriptions_count", len(reg.Subscriptions)),
	)

	return serviceInfo
}

// Unregister removes a service from the registry
func (r *Registry) Unregister(serviceName, podName string) *models.ServiceInfo {
	key := serviceName + ":" + podName

	logger.Debug("Registry: Unregister called",
		zap.String("service_key", key),
	)

	service, err := r.store.GetService(r.ctx, key)
	if err != nil {
		logger.Warn("Registry: Service not found for unregistration",
			zap.String("service_key", key),
			zap.Error(err),
		)
		return nil
	}

	// Remove subscriptions
	if len(service.Subscriptions) > 0 {
		logger.Debug("Registry: Removing subscriptions",
			zap.String("service_key", key),
			zap.Int("subscriptions_count", len(service.Subscriptions)),
		)
		r.removeSubscriptions(key, service.Subscriptions)
	}

	// Remove from storage
	if err := r.store.DeleteService(r.ctx, key); err != nil {
		logger.Error("Registry: Failed to delete service from storage",
			zap.String("service_key", key),
			zap.Error(err),
		)
	} else {
		logger.Debug("Registry: Service deleted from storage",
			zap.String("service_key", key),
		)
	}

	logger.Info("Registry: Service unregistered successfully",
		zap.String("service_key", key),
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

// UpdateHealthStatus updates the health status of a service
func (r *Registry) UpdateHealthStatus(key string, status models.ServiceStatus) bool {
	logger.Debug("Registry: UpdateHealthStatus called",
		zap.String("service_key", key),
		zap.String("new_status", string(status)),
	)

	service, err := r.store.GetService(r.ctx, key)
	if err != nil {
		logger.Warn("Registry: Service not found for health status update",
			zap.String("service_key", key),
			zap.Error(err),
		)
		return false
	}

	oldStatus := service.Status
	timestamp := time.Now()

	// Update in storage
	if err := r.store.UpdateHealthStatus(r.ctx, key, status, timestamp); err != nil {
		logger.Error("Registry: Failed to update health status in storage",
			zap.String("service_key", key),
			zap.String("old_status", string(oldStatus)),
			zap.String("new_status", string(status)),
			zap.Error(err),
		)
		return false
	}

	statusChanged := oldStatus != status
	if statusChanged {
		logger.Info("Registry: Health status updated",
			zap.String("service_key", key),
			zap.String("old_status", string(oldStatus)),
			zap.String("new_status", string(status)),
		)
	} else {
		logger.Debug("Registry: Health status unchanged",
			zap.String("service_key", key),
			zap.String("status", string(status)),
		)
	}

	return statusChanged
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
			logger.Error("Registry: Failed to add subscription",
				zap.String("subscriber_key", subscriberKey),
				zap.String("service_name", sub.ServiceName),
				zap.Error(err),
			)
		} else {
			logger.Debug("Registry: Subscription added",
				zap.String("subscriber_key", subscriberKey),
				zap.String("service_name", sub.ServiceName),
			)
		}
	}
}

// removeSubscriptions removes subscriptions for a service
func (r *Registry) removeSubscriptions(subscriberKey string, subscriptions []models.Subscription) {
	for _, sub := range subscriptions {
		if err := r.store.RemoveSubscription(r.ctx, subscriberKey, sub.ServiceName); err != nil {
			logger.Error("Registry: Failed to remove subscription",
				zap.String("subscriber_key", subscriberKey),
				zap.String("service_name", sub.ServiceName),
				zap.Error(err),
			)
		} else {
			logger.Debug("Registry: Subscription removed",
				zap.String("subscriber_key", subscriberKey),
				zap.String("service_name", sub.ServiceName),
			)
		}
	}
}
