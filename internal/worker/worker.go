package worker

import (
	"context"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/events"
	"github.com/chronnie/governance/internal/notifier"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
	"go.uber.org/zap"
)

// EventWorker processes events from the queue using handlers
type EventWorker struct {
	registry      *registry.Registry
	notifier      *notifier.Notifier
	healthChecker *notifier.HealthChecker
	dualStore     *storage.DualStore // For database sync during reconciliation
}

// NewEventWorker creates a new event worker
func NewEventWorker(
	reg *registry.Registry,
	notif *notifier.Notifier,
	healthCheck *notifier.HealthChecker,
	dualStore *storage.DualStore,
) *EventWorker {
	return &EventWorker{
		registry:      reg,
		notifier:      notif,
		healthChecker: healthCheck,
		dualStore:     dualStore,
	}
}

// RegisterHandlers registers all event handlers to the queue
func (w *EventWorker) RegisterHandlers(queue eventqueue.IEventQueue) {
	// Register handler for each event type
	queue.RegisterHandler(string(events.EventRegister), eventqueue.EventHandlerFunc(w.handleRegister))
	queue.RegisterHandler(string(events.EventUnregister), eventqueue.EventHandlerFunc(w.handleUnregister))
	queue.RegisterHandler(string(events.EventHealthCheck), eventqueue.EventHandlerFunc(w.handleHealthCheck))
	queue.RegisterHandler(string(events.EventReconcile), eventqueue.EventHandlerFunc(w.handleReconcile))
}

// handleRegister processes service registration
func (w *EventWorker) handleRegister(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	registerEvent, ok := eventData.(*events.RegisterEvent)
	if !ok {
		logger.Warn("Invalid event data type for register event")
		return nil
	}

	logger.Info("Processing register event",
		zap.String("service_name", registerEvent.Registration.ServiceName),
		zap.String("pod_name", registerEvent.Registration.PodName),
		zap.String("health_check_url", registerEvent.Registration.HealthCheckURL),
	)

	// Register service in registry
	serviceInfo := w.registry.Register(registerEvent.Registration)
	logger.Debug("Service registered in registry",
		zap.String("service_key", serviceInfo.GetKey()),
		zap.String("service_name", serviceInfo.ServiceName),
		zap.String("pod_name", serviceInfo.PodName),
	)

	// Get all pods of this service
	servicePods := w.registry.GetByServiceName(serviceInfo.ServiceName)
	logger.Debug("Retrieved service pods",
		zap.String("service_name", serviceInfo.ServiceName),
		zap.Int("pod_count", len(servicePods)),
	)

	// Send result to caller if result channel is provided
	if registerEvent.ResultChan != nil {
		// Build pod info list for the registered service
		podInfoList := make([]models.PodInfo, 0, len(servicePods))
		for _, pod := range servicePods {
			podInfoList = append(podInfoList, models.PodInfo{
				PodName:   pod.PodName,
				Status:    pod.Status,
				Providers: pod.Providers,
			})
		}

		// Build subscribed services map
		subscribedServices := make(map[string][]models.PodInfo)
		if len(registerEvent.Registration.Subscriptions) > 0 {
			// Convert subscriptions to service names for logging
			subNames := make([]string, len(registerEvent.Registration.Subscriptions))
			for i, sub := range registerEvent.Registration.Subscriptions {
				subNames[i] = sub.ServiceName
			}
			logger.Debug("Collecting subscribed services pod info",
				zap.String("service_key", serviceInfo.GetKey()),
				zap.Strings("subscriptions", subNames),
			)

			for _, sub := range registerEvent.Registration.Subscriptions {
				pods := w.registry.GetByServiceName(sub.ServiceName)
				if len(pods) > 0 {
					podList := make([]models.PodInfo, 0, len(pods))
					for _, pod := range pods {
						podList = append(podList, models.PodInfo{
							PodName:   pod.PodName,
							Status:    pod.Status,
							Providers: pod.Providers,
						})
					}
					subscribedServices[sub.ServiceName] = podList
					logger.Debug("Collected subscribed service pods",
						zap.String("subscribed_service", sub.ServiceName),
						zap.Int("pod_count", len(podList)),
					)
				} else {
					logger.Debug("No pods found for subscribed service",
						zap.String("subscribed_service", sub.ServiceName),
					)
				}
			}
		}

		registerEvent.ResultChan <- &events.RegisterResult{
			Pods:               podInfoList,
			SubscribedServices: subscribedServices,
			Error:              nil,
		}
		close(registerEvent.ResultChan)
	}

	// Build notification payload
	payload := notifier.BuildNotificationPayload(
		serviceInfo.ServiceName,
		models.EventTypeRegister,
		servicePods,
	)

	// Notify all subscribers of this service
	subscribers := w.registry.GetSubscriberServices(serviceInfo.ServiceName)
	logger.Info("Notifying subscribers of service registration",
		zap.String("service_name", serviceInfo.ServiceName),
		zap.Int("subscriber_count", len(subscribers)),
	)
	w.notifier.NotifySubscribers(subscribers, payload)

	return nil
}

// handleUnregister processes service unregistration
func (w *EventWorker) handleUnregister(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	unregisterEvent, ok := eventData.(*events.UnregisterEvent)
	if !ok {
		logger.Warn("Invalid event data type for unregister event")
		return nil
	}

	logger.Info("Processing unregister event",
		zap.String("service_name", unregisterEvent.ServiceName),
		zap.String("pod_name", unregisterEvent.PodName),
	)

	// Unregister service from registry
	serviceInfo := w.registry.Unregister(unregisterEvent.ServiceName, unregisterEvent.PodName)
	if serviceInfo == nil {
		logger.Warn("Service not found for unregistration",
			zap.String("service_name", unregisterEvent.ServiceName),
			zap.String("pod_name", unregisterEvent.PodName),
		)
		return nil
	}

	logger.Debug("Service unregistered from registry",
		zap.String("service_key", serviceInfo.GetKey()),
		zap.String("service_name", serviceInfo.ServiceName),
		zap.String("pod_name", serviceInfo.PodName),
	)

	// Get remaining pods of this service (after unregistration)
	servicePods := w.registry.GetByServiceName(unregisterEvent.ServiceName)
	logger.Debug("Retrieved remaining service pods",
		zap.String("service_name", unregisterEvent.ServiceName),
		zap.Int("remaining_pod_count", len(servicePods)),
	)

	// Build notification payload
	payload := notifier.BuildNotificationPayload(
		unregisterEvent.ServiceName,
		models.EventTypeUnregister,
		servicePods,
	)

	// Notify all subscribers of this service
	subscribers := w.registry.GetSubscriberServices(unregisterEvent.ServiceName)
	logger.Info("Notifying subscribers of service unregistration",
		zap.String("service_name", unregisterEvent.ServiceName),
		zap.Int("subscriber_count", len(subscribers)),
	)
	w.notifier.NotifySubscribers(subscribers, payload)

	return nil
}

// handleHealthCheck processes health check event
func (w *EventWorker) handleHealthCheck(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	healthCheckEvent, ok := eventData.(*events.HealthCheckEvent)
	if !ok {
		logger.Warn("Invalid event data type for health check event")
		return nil
	}

	logger.Debug("Processing health check event",
		zap.String("service_key", healthCheckEvent.ServiceKey),
	)

	// Get service from registry
	serviceInfo, exists := w.registry.Get(healthCheckEvent.ServiceKey)
	if !exists {
		logger.Warn("Service not found for health check",
			zap.String("service_key", healthCheckEvent.ServiceKey),
		)
		return nil
	}

	logger.Debug("Performing health check",
		zap.String("service_name", serviceInfo.ServiceName),
		zap.String("pod_name", serviceInfo.PodName),
		zap.String("health_check_url", serviceInfo.HealthCheckURL),
		zap.String("current_status", string(serviceInfo.Status)),
	)

	// Perform health check with retries
	newStatus := w.healthChecker.GetHealthStatus(serviceInfo.HealthCheckURL)

	logger.Debug("Health check completed",
		zap.String("service_key", healthCheckEvent.ServiceKey),
		zap.String("new_status", string(newStatus)),
	)

	// Update health status in registry
	statusChanged := w.registry.UpdateHealthStatus(healthCheckEvent.ServiceKey, newStatus)

	// If status changed, notify subscribers
	if statusChanged {
		logger.Info("Service health status changed",
			zap.String("service_name", serviceInfo.ServiceName),
			zap.String("pod_name", serviceInfo.PodName),
			zap.String("new_status", string(newStatus)),
		)

		// Get all pods of this service
		servicePods := w.registry.GetByServiceName(serviceInfo.ServiceName)

		// Build notification payload
		payload := notifier.BuildNotificationPayload(
			serviceInfo.ServiceName,
			models.EventTypeUpdate,
			servicePods,
		)

		// Notify all subscribers
		subscribers := w.registry.GetSubscriberServices(serviceInfo.ServiceName)
		logger.Info("Notifying subscribers of health status change",
			zap.String("service_name", serviceInfo.ServiceName),
			zap.Int("subscriber_count", len(subscribers)),
		)
		w.notifier.NotifySubscribers(subscribers, payload)
	} else {
		logger.Debug("Health status unchanged",
			zap.String("service_key", healthCheckEvent.ServiceKey),
			zap.String("status", string(newStatus)),
		)
	}

	return nil
}

// handleReconcile processes reconcile event (notify all subscribers with current state + sync database)
func (w *EventWorker) handleReconcile(ctx context.Context, event eventqueue.IEvent) error {
	logger.Info("Processing reconcile event - starting full reconciliation")

	// Sync from database to cache (if database is enabled)
	// This ensures cache has the latest data from database
	if w.dualStore.GetDatabase() != nil {
		logger.Info("Database persistence enabled - syncing from database to cache")
		servicesSynced, subsSynced, err := w.dualStore.SyncFromDatabase(ctx)
		if err != nil {
			logger.Error("Failed to sync from database", zap.Error(err))
		} else {
			logger.Info("Database sync completed successfully",
				zap.Int("services_synced", servicesSynced),
				zap.Int("subscriptions_synced", subsSynced),
			)
		}
	} else {
		logger.Debug("Database persistence disabled - using cache only")
	}

	// Get all services from cache
	allServices := w.registry.GetAllServices()
	logger.Info("Retrieved all services from cache",
		zap.Int("total_services", len(allServices)),
	)

	// Group services by service name
	serviceGroups := make(map[string][]*models.ServiceInfo)
	for _, service := range allServices {
		serviceGroups[service.ServiceName] = append(serviceGroups[service.ServiceName], service)
	}

	logger.Info("Grouped services by service name",
		zap.Int("service_groups", len(serviceGroups)),
	)

	// For each service group, notify all subscribers
	totalNotifications := 0
	for serviceName, pods := range serviceGroups {
		logger.Debug("Processing service group for reconciliation",
			zap.String("service_name", serviceName),
			zap.Int("pod_count", len(pods)),
		)

		// Build notification payload
		payload := notifier.BuildNotificationPayload(
			serviceName,
			models.EventTypeReconcile,
			pods,
		)

		// Get subscribers
		subscribers := w.registry.GetSubscriberServices(serviceName)
		if len(subscribers) > 0 {
			logger.Info("Notifying subscribers for service reconciliation",
				zap.String("service_name", serviceName),
				zap.Int("pod_count", len(pods)),
				zap.Int("subscriber_count", len(subscribers)),
			)
			w.notifier.NotifySubscribers(subscribers, payload)
			totalNotifications += len(subscribers)
		} else {
			logger.Debug("No subscribers for service",
				zap.String("service_name", serviceName),
			)
		}
	}

	logger.Info("Reconciliation completed",
		zap.Int("service_groups", len(serviceGroups)),
		zap.Int("total_notifications_sent", totalNotifications),
	)

	return nil
}
