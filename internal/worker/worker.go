package worker

import (
	"context"
	"log"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/events"
	"github.com/chronnie/governance/internal/notifier"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/models"
)

// EventWorker processes events from the queue using handlers
type EventWorker struct {
	registry      *registry.Registry
	notifier      *notifier.Notifier
	healthChecker *notifier.HealthChecker
}

// NewEventWorker creates a new event worker
func NewEventWorker(
	reg *registry.Registry,
	notif *notifier.Notifier,
	healthCheck *notifier.HealthChecker,
) *EventWorker {
	return &EventWorker{
		registry:      reg,
		notifier:      notif,
		healthChecker: healthCheck,
	}
}

// RegisterHandlers registers all event handlers to the queue
func (w *EventWorker) RegisterHandlers(queue eventqueue.IEventQueue) {
	// Register handler for each event type
	queue.RegisterHandler(string(events.EventRegister), eventqueue.EventHandlerFunc(w.handleRegister))
	queue.RegisterHandler(string(events.EventUnregister), eventqueue.EventHandlerFunc(w.handleUnregister))
	queue.RegisterHandler(string(events.EventHealthCheck), eventqueue.EventHandlerFunc(w.handleHealthCheck))
	queue.RegisterHandler(string(events.EventReconcile), eventqueue.EventHandlerFunc(w.handleReconcile))

	log.Println("[EventWorker] All event handlers registered")
}

// handleRegister processes service registration
func (w *EventWorker) handleRegister(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	registerEvent, ok := eventData.(*events.RegisterEvent)
	if !ok {
		log.Printf("[EventWorker] Invalid event data type for RegisterEvent")
		return nil
	}
	log.Printf("[EventWorker] Processing RegisterEvent: service=%s, pod=%s",
		registerEvent.Registration.ServiceName, registerEvent.Registration.PodName)

	// Register service in registry
	serviceInfo := w.registry.Register(registerEvent.Registration)

	// Get all pods of this service
	servicePods := w.registry.GetByServiceName(serviceInfo.ServiceName)

	// Build notification payload
	payload := notifier.BuildNotificationPayload(
		serviceInfo.ServiceName,
		models.EventTypeRegister,
		servicePods,
	)

	// Notify all subscribers of this service
	subscribers := w.registry.GetSubscriberServices(serviceInfo.ServiceName)
	w.notifier.NotifySubscribers(subscribers, payload)

	log.Printf("[EventWorker] RegisterEvent completed: service=%s, pod=%s, subscribers=%d",
		serviceInfo.ServiceName, serviceInfo.PodName, len(subscribers))

	return nil
}

// handleUnregister processes service unregistration
func (w *EventWorker) handleUnregister(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	unregisterEvent, ok := eventData.(*events.UnregisterEvent)
	if !ok {
		log.Printf("[EventWorker] Invalid event data type for UnregisterEvent")
		return nil
	}

	log.Printf("[EventWorker] Processing UnregisterEvent: service=%s, pod=%s",
		unregisterEvent.ServiceName, unregisterEvent.PodName)

	// Unregister service from registry
	serviceInfo := w.registry.Unregister(unregisterEvent.ServiceName, unregisterEvent.PodName)
	if serviceInfo == nil {
		log.Printf("[EventWorker] UnregisterEvent failed: service not found (service=%s, pod=%s)",
			unregisterEvent.ServiceName, unregisterEvent.PodName)
		return nil
	}

	// Get remaining pods of this service (after unregistration)
	servicePods := w.registry.GetByServiceName(unregisterEvent.ServiceName)

	// Build notification payload
	payload := notifier.BuildNotificationPayload(
		unregisterEvent.ServiceName,
		models.EventTypeUnregister,
		servicePods,
	)

	// Notify all subscribers of this service
	subscribers := w.registry.GetSubscriberServices(unregisterEvent.ServiceName)
	w.notifier.NotifySubscribers(subscribers, payload)

	log.Printf("[EventWorker] UnregisterEvent completed: service=%s, pod=%s, remaining_pods=%d, subscribers=%d",
		unregisterEvent.ServiceName, unregisterEvent.PodName, len(servicePods), len(subscribers))

	return nil
}

// handleHealthCheck processes health check event
func (w *EventWorker) handleHealthCheck(ctx context.Context, event eventqueue.IEvent) error {
	eventData := events.GetEventData(ctx)
	healthCheckEvent, ok := eventData.(*events.HealthCheckEvent)
	if !ok {
		log.Printf("[EventWorker] Invalid event data type for HealthCheckEvent")
		return nil
	}

	log.Printf("[EventWorker] Processing HealthCheckEvent: key=%s", healthCheckEvent.ServiceKey)

	// Get service from registry
	serviceInfo, exists := w.registry.Get(healthCheckEvent.ServiceKey)
	if !exists {
		log.Printf("[EventWorker] HealthCheckEvent skipped: service not found (key=%s)", healthCheckEvent.ServiceKey)
		return nil
	}

	// Perform health check with retries
	newStatus := w.healthChecker.GetHealthStatus(serviceInfo.HealthCheckURL)

	// Update health status in registry
	statusChanged := w.registry.UpdateHealthStatus(healthCheckEvent.ServiceKey, newStatus)

	log.Printf("[EventWorker] HealthCheckEvent completed: key=%s, status=%s, changed=%v",
		healthCheckEvent.ServiceKey, newStatus, statusChanged)

	// If status changed, notify subscribers
	if statusChanged {
		log.Printf("[EventWorker] Health status changed for %s: %s -> %s",
			healthCheckEvent.ServiceKey, serviceInfo.Status, newStatus)

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
		w.notifier.NotifySubscribers(subscribers, payload)

		log.Printf("[EventWorker] Health status change notification sent: service=%s, subscribers=%d",
			serviceInfo.ServiceName, len(subscribers))
	}

	return nil
}

// handleReconcile processes reconcile event (notify all subscribers with current state)
func (w *EventWorker) handleReconcile(ctx context.Context, event eventqueue.IEvent) error {
	log.Println("[EventWorker] Processing ReconcileEvent: notifying all subscribers")

	// Get all services
	allServices := w.registry.GetAllServices()

	// Group services by service name
	serviceGroups := make(map[string][]*models.ServiceInfo)
	for _, service := range allServices {
		serviceGroups[service.ServiceName] = append(serviceGroups[service.ServiceName], service)
	}

	// For each service group, notify all subscribers
	totalNotifications := 0
	for serviceName, pods := range serviceGroups {
		// Build notification payload
		payload := notifier.BuildNotificationPayload(
			serviceName,
			models.EventTypeReconcile,
			pods,
		)

		// Get subscribers
		subscribers := w.registry.GetSubscriberServices(serviceName)
		if len(subscribers) > 0 {
			w.notifier.NotifySubscribers(subscribers, payload)
			totalNotifications += len(subscribers)
			log.Printf("[EventWorker] Reconcile notification sent: service=%s, pods=%d, subscribers=%d",
				serviceName, len(pods), len(subscribers))
		}
	}

	log.Printf("[EventWorker] ReconcileEvent completed: service_groups=%d, total_notifications=%d",
		len(serviceGroups), totalNotifications)

	return nil
}
