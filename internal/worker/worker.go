package worker

import (
	"context"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/events"
	"github.com/chronnie/governance/internal/auditor"
	"github.com/chronnie/governance/internal/notifier"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
)

// EventWorker processes events from the queue using handlers
type EventWorker struct {
	registry      *registry.Registry
	notifier      *notifier.Notifier
	healthChecker *notifier.HealthChecker
	dualStore     *storage.DualStore // For database sync during reconciliation
	auditor       *auditor.Auditor   // For audit logging
	config        *models.ManagerConfig
	logger        logger.Logger
}

// NewEventWorker creates a new event worker
func NewEventWorker(
	reg *registry.Registry,
	notif *notifier.Notifier,
	healthCheck *notifier.HealthChecker,
	dualStore *storage.DualStore,
	aud *auditor.Auditor,
	cfg *models.ManagerConfig,
	log logger.Logger,
) *EventWorker {
	return &EventWorker{
		registry:      reg,
		notifier:      notif,
		healthChecker: healthCheck,
		dualStore:     dualStore,
		auditor:       aud,
		config:        cfg,
		logger:        log,
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
		w.logger.Warnw("Invalid event data type for register event")
		return nil
	}

	w.logger.Infow("Processing register event",
		"service_name", registerEvent.Registration.ServiceName,
		"pod_name", registerEvent.Registration.PodName,
		"health_check_url", registerEvent.Registration.HealthCheckURL,
	)

	// Register service in registry
	serviceInfo := w.registry.Register(registerEvent.Registration)
	w.logger.Debugw("Service registered in registry",
		"service_key", serviceInfo.GetKey(),
		"service_name", serviceInfo.ServiceName,
		"pod_name", serviceInfo.PodName,
	)

	// Log audit event
	w.auditor.LogRegister(ctx, serviceInfo.ServiceName, serviceInfo.PodName, models.AuditResultSuccess, map[string]interface{}{
		"health_check_url": serviceInfo.HealthCheckURL,
		"notification_url": serviceInfo.NotificationURL,
		"providers_count":  len(serviceInfo.Providers),
		"subscriptions":    len(serviceInfo.Subscriptions),
	}, "")

	// Get all pods of this service
	servicePods := w.registry.GetByServiceName(serviceInfo.ServiceName)
	w.logger.Debugw("Retrieved service pods",
		"service_name", serviceInfo.ServiceName,
		"pod_count", len(servicePods),
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
			w.logger.Debugw("Collecting subscribed services pod info",
				"service_key", serviceInfo.GetKey(),
				"subscriptions", subNames,
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
					w.logger.Debugw("Collected subscribed service pods",
						"subscribed_service", sub.ServiceName,
						"pod_count", len(podList),
					)
				} else {
					w.logger.Debugw("No pods found for subscribed service",
						"subscribed_service", sub.ServiceName,
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

	// Notify all subscribers of this service (in a separate goroutine to avoid blocking)
	subscribers := w.registry.GetSubscriberServices(serviceInfo.ServiceName)
	w.logger.Infow("Notifying subscribers of service registration",
		"service_name", serviceInfo.ServiceName,
		"subscriber_count", len(subscribers),
	)
	go w.notifier.NotifySubscribers(subscribers, payload)

	return nil
}

// handleUnregister processes service unregistration
func (w *EventWorker) handleUnregister(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	serviceInfo, ok := eventData.(*events.UnregisterEvent)
	if !ok {
		w.logger.Warnw("Invalid event data type for unregister event")
		return nil
	}

	w.logger.Infow("Processing unregister event",
		"service_name", serviceInfo.ServiceName,
		"pod_name", serviceInfo.PodName,
	)
	// Get subscribers before unregistering (for notification)
	subscribers := w.registry.GetSubscriberServices(serviceInfo.ServiceName)

	// Unregister the failed service - this deletes from BOTH cache AND database
	deletedService := w.registry.Unregister(serviceInfo.ServiceName, serviceInfo.PodName)
	if deletedService != nil {
		w.logger.Infow("Pod removed from management list (cache + database) after repeated failures",
			"service_name", serviceInfo.ServiceName,
			"pod_name", serviceInfo.PodName,
		)

		// Log auto-cleanup audit event
		w.auditor.LogUnregister(ctx, serviceInfo.ServiceName, serviceInfo.PodName, models.AuditResultSuccess, map[string]interface{}{
			"failure_limit":         w.config.HealthCheckFailureLimit,
			"consecutive_failures":  deletedService.ConsecutiveFailures,
			"failure_reason":        "exceeded health check failure limit",
		}, "")

		// Get remaining pods of this service (after deletion)
		remainingPods := w.registry.GetByServiceName(serviceInfo.ServiceName)

		// Notify subscribers about the deletion
		payload := notifier.BuildNotificationPayload(
			serviceInfo.ServiceName,
			models.EventTypeUnregister,
			remainingPods,
		)

		w.logger.Infow("Notifying subscribers of auto-cleanup",
			"service_name", serviceInfo.ServiceName,
			"subscriber_count", len(subscribers),
			"remaining_pods", len(remainingPods),
		)
		go w.notifier.NotifySubscribers(subscribers, payload)
	}

	return nil
}

// handleHealthCheck processes health check event
func (w *EventWorker) handleHealthCheck(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	healthCheckEvent, ok := eventData.(*events.HealthCheckEvent)
	if !ok {
		w.logger.Warnw("Invalid event data type for health check event")
		return nil
	}

	w.logger.Debugw("Processing health check event",
		"service_key", healthCheckEvent.ServiceKey,
	)

	// Get service from registry
	serviceInfo, exists := w.registry.Get(healthCheckEvent.ServiceKey)
	if !exists {
		w.logger.Warnw("Service not found for health check",
			"service_key", healthCheckEvent.ServiceKey,
		)
		return nil
	}

	w.logger.Debugw("Performing health check",
		"service_name", serviceInfo.ServiceName,
		"pod_name", serviceInfo.PodName,
		"health_check_url", serviceInfo.HealthCheckURL,
		"current_status", string(serviceInfo.Status),
	)

	// Perform health check with retries
	healthCheckPassed := w.healthChecker.CheckHealth(serviceInfo.HealthCheckURL)

	w.logger.Debugw("Health check completed",
		"service_key", healthCheckEvent.ServiceKey,
		"health_check_passed", healthCheckPassed,
	)

	// Log heartbeat audit event
	heartbeatResult := models.AuditResultSuccess
	if !healthCheckPassed {
		heartbeatResult = models.AuditResultFailure
	}

	previousStatus := serviceInfo.Status
	newStatus := models.StatusHealthy
	if !healthCheckPassed {
		newStatus = models.StatusUnhealthy
	}

	w.auditor.LogHeartbeat(ctx, serviceInfo.ServiceName, serviceInfo.PodName, previousStatus, newStatus, heartbeatResult, map[string]interface{}{
		"health_check_url": serviceInfo.HealthCheckURL,
	})

	// If health check FAILED, track consecutive failures and check auto-cleanup threshold
	if !healthCheckPassed {
		// Track the failure (in-memory only, not persisted to storage)
		consecutiveFailures := w.registry.TrackHealthCheckFailure(healthCheckEvent.ServiceKey)

		w.logger.Warnw("Health check failed",
			"service_key", healthCheckEvent.ServiceKey,
			"consecutive_failures", consecutiveFailures,
			"failure_limit", w.config.HealthCheckFailureLimit,
		)

		// Get subscribers before unregistering (for notification)
		subscribers := w.registry.GetSubscriberServices(serviceInfo.ServiceName)

		// Unregister the failed service - this deletes from BOTH cache AND database
		deletedService := w.registry.Unregister(serviceInfo.ServiceName, serviceInfo.PodName)
		if deletedService != nil {
			w.logger.Infow("Pod removed from management list (cache + database) after repeated failures",
				"service_name", serviceInfo.ServiceName,
				"pod_name", serviceInfo.PodName,
				"consecutive_failures", consecutiveFailures,
			)

			// Log auto-cleanup audit event
			w.auditor.LogAutoCleanup(ctx, serviceInfo.ServiceName, serviceInfo.PodName, consecutiveFailures, "exceeded health check failure limit", map[string]interface{}{
				"failure_limit":    w.config.HealthCheckFailureLimit,
				"health_check_url": serviceInfo.HealthCheckURL,
			})

			// Get remaining pods of this service (after deletion)
			remainingPods := w.registry.GetByServiceName(serviceInfo.ServiceName)

			// Notify subscribers about the deletion
			payload := notifier.BuildNotificationPayload(
				serviceInfo.ServiceName,
				models.EventTypeUnregister,
				remainingPods,
			)

			w.logger.Infow("Notifying subscribers of auto-cleanup",
				"service_name", serviceInfo.ServiceName,
				"subscriber_count", len(subscribers),
				"remaining_pods", len(remainingPods),
			)
			go w.notifier.NotifySubscribers(subscribers, payload)
		}

		// Health check failed but not yet at threshold - just return
		return nil
	}

	// Health check PASSED - reset failure counter
	w.registry.ResetHealthCheckFailures(healthCheckEvent.ServiceKey)

	w.logger.Debugw("Health check passed",
		"service_key", healthCheckEvent.ServiceKey,
	)

	return nil
}

// handleReconcile processes reconcile event (notify all subscribers with current state + sync database)
func (w *EventWorker) handleReconcile(ctx context.Context, event eventqueue.IEvent) error {
	w.logger.Infow("Processing reconcile event - starting full reconciliation")

	// Sync from database to cache (if database is enabled)
	// This ensures cache has the latest data from database
	if w.dualStore.GetDatabase() != nil {
		w.logger.Infow("Database persistence enabled - syncing from database to cache")
		servicesSynced, subsSynced, err := w.dualStore.SyncFromDatabase(ctx)
		if err != nil {
			w.logger.Errorw("Failed to sync from database", "error", err)
		} else {
			w.logger.Infow("Database sync completed successfully",
				"services_synced", servicesSynced,
				"subscriptions_synced", subsSynced,
			)
		}
	} else {
		w.logger.Debugw("Database persistence disabled - using cache only")
	}

	// Get all services from cache
	allServices := w.registry.GetAllServices()
	w.logger.Infow("Retrieved all services from cache",
		"total_services", len(allServices),
	)

	// Group services by service name
	serviceGroups := make(map[string][]*models.ServiceInfo)
	for _, service := range allServices {
		serviceGroups[service.ServiceName] = append(serviceGroups[service.ServiceName], service)
	}

	w.logger.Infow("Grouped services by service name",
		"service_groups", len(serviceGroups),
	)

	// For each service group, notify all subscribers
	totalNotifications := 0
	for serviceName, pods := range serviceGroups {
		w.logger.Debugw("Processing service group for reconciliation",
			"service_name", serviceName,
			"pod_count", len(pods),
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
			w.logger.Infow("Notifying subscribers for service reconciliation",
				"service_name", serviceName,
				"pod_count", len(pods),
				"subscriber_count", len(subscribers),
			)
			go w.notifier.NotifySubscribers(subscribers, payload)
			totalNotifications += len(subscribers)
		} else {
			w.logger.Debugw("No subscribers for service",
				"service_name", serviceName,
			)
		}
	}

	w.logger.Infow("Reconciliation completed",
		"service_groups", len(serviceGroups),
		"total_notifications_sent", totalNotifications,
	)

	// Log reconcile audit event
	w.auditor.LogReconcile(ctx, models.AuditResultSuccess, map[string]interface{}{
		"service_groups":     len(serviceGroups),
		"total_services":     len(allServices),
		"notifications_sent": totalNotifications,
	})

	return nil
}
