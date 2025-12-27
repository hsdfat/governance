package api

import (
	"encoding/json"
	"net/http"
	"time"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/events"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
)

// Handler handles HTTP requests for the governance manager
type Handler struct {
	registry   *registry.Registry
	eventQueue eventqueue.IEventQueue
	logger     logger.Logger
}

// NewHandler creates a new API handler
func NewHandler(reg *registry.Registry, eventQueue eventqueue.IEventQueue, log logger.Logger) *Handler {
	return &Handler{
		registry:   reg,
		eventQueue: eventQueue,
		logger:     log,
	}
}

// RegisterHandler handles POST /register requests
func (h *Handler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Infow("API: Received register request",
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	if r.Method != http.MethodPost {
		h.logger.Warnw("API: Invalid method for register endpoint",
			"method", r.Method,
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var registration models.ServiceRegistration
	if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
		h.logger.Errorw("API: Failed to decode registration request",
			"error", err,
			"remote_addr", r.RemoteAddr,
		)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	h.logger.Debugw("API: Parsed registration request",
		"service_name", registration.ServiceName,
		"pod_name", registration.PodName,
		"providers_count", len(registration.Providers),
		"subscriptions_count", len(registration.Subscriptions),
	)

	// Validate registration
	if err := h.validateRegistration(&registration); err != nil {
		h.logger.Warnw("API: Invalid registration request",
			"service_name", registration.ServiceName,
			"pod_name", registration.PodName,
			"error", err,
		)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.logger.Infow("API: Registration validated successfully",
		"service_name", registration.ServiceName,
		"pod_name", registration.PodName,
	)

	// Create result channel to receive pod list
	resultChan := make(chan *events.RegisterResult, 1)

	// Create context with event data and result channel
	ctx := events.NewRegisterContextWithResult(&registration, resultChan)

	// Create and enqueue register event (with deadline for register events)
	event := eventqueue.NewEvent(string(events.EventRegister), ctx, eventqueue.WithTimeout(5*time.Second))

	if err := h.eventQueue.Enqueue(event); err != nil {
		h.logger.Errorw("API: Failed to enqueue register event",
			"service_name", registration.ServiceName,
			"pod_name", registration.PodName,
			"error", err,
		)
		http.Error(w, "Failed to process registration", http.StatusInternalServerError)
		return
	}

	h.logger.Infow("API: Register event enqueued successfully, waiting for result",
		"service_name", registration.ServiceName,
		"pod_name", registration.PodName,
	)

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		if result.Error != nil {
			h.logger.Errorw("API: Registration processing failed",
				"service_name", registration.ServiceName,
				"pod_name", registration.PodName,
				"error", result.Error,
			)
			http.Error(w, result.Error.Error(), http.StatusInternalServerError)
			return
		}

		h.logger.Infow("API: Registration completed successfully",
			"service_name", registration.ServiceName,
			"pod_name", registration.PodName,
			"pod_count", len(result.Pods),
			"subscribed_services_count", len(result.SubscribedServices),
		)

		// Return success response with pod list and subscribed services
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(models.RegistrationResponse{
			Status:             "success",
			Message:            "Registration completed successfully",
			ServiceName:        registration.ServiceName,
			Pods:               result.Pods,
			SubscribedServices: result.SubscribedServices,
		})

		h.logger.Debugw("API: Sent success response with pod info and subscribed services",
			"service_name", registration.ServiceName,
			"pod_name", registration.PodName,
			"pod_count", len(result.Pods),
			"subscribed_services_count", len(result.SubscribedServices),
		)

	case <-time.After(10 * time.Second):
		h.logger.Errorw("API: Registration processing timeout",
			"service_name", registration.ServiceName,
			"pod_name", registration.PodName,
		)
		http.Error(w, "Registration processing timeout", http.StatusGatewayTimeout)
	}
}

// UnregisterHandler handles DELETE /unregister requests
func (h *Handler) UnregisterHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Infow("API: Received unregister request",
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	if r.Method != http.MethodDelete {
		h.logger.Warnw("API: Invalid method for unregister endpoint",
			"method", r.Method,
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	serviceName := r.URL.Query().Get("service_name")
	podName := r.URL.Query().Get("pod_name")

	if serviceName == "" || podName == "" {
		h.logger.Warnw("API: Missing required query parameters",
			"service_name", serviceName,
			"pod_name", podName,
		)
		http.Error(w, "Missing service_name or pod_name query parameters", http.StatusBadRequest)
		return
	}

	h.logger.Infow("API: Unregister request validated",
		"service_name", serviceName,
		"pod_name", podName,
	)

	// Create context with event data
	ctx := events.NewUnregisterContext(serviceName, podName)

	// Create and enqueue unregister event (with deadline for unregister events)
	event := eventqueue.NewEvent(string(events.EventUnregister), ctx, eventqueue.WithTimeout(5*time.Second))

	if err := h.eventQueue.Enqueue(event); err != nil {
		h.logger.Errorw("API: Failed to enqueue unregister event",
			"service_name", serviceName,
			"pod_name", podName,
			"error", err,
		)
		http.Error(w, "Failed to process unregistration", http.StatusInternalServerError)
		return
	}

	h.logger.Infow("API: Unregister event enqueued successfully",
		"service_name", serviceName,
		"pod_name", podName,
	)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Unregistration event queued successfully",
	})

	h.logger.Debugw("API: Sent success response for unregistration",
		"service_name", serviceName,
		"pod_name", podName,
	)
}

// ServicesHandler handles GET /services requests (for debugging)
func (h *Handler) ServicesHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debugw("API: Received services query request",
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	if r.Method != http.MethodGet {
		h.logger.Warnw("API: Invalid method for services endpoint",
			"method", r.Method,
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	services := h.registry.GetAllServices()

	h.logger.Infow("API: Retrieved all services",
		"service_count", len(services),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":    len(services),
		"services": services,
	})

	h.logger.Debugw("API: Sent services response",
		"service_count", len(services),
	)
}

// HealthHandler handles GET /health requests
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debugw("API: Received health check request",
		"remote_addr", r.RemoteAddr,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// HeartbeatHandler handles POST /heartbeat requests
func (h *Handler) HeartbeatHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Debugw("API: Received heartbeat request",
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	if r.Method != http.MethodPost {
		h.logger.Warnw("API: Invalid method for heartbeat endpoint",
			"method", r.Method,
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	serviceName := r.URL.Query().Get("service_name")
	podName := r.URL.Query().Get("pod_name")

	if serviceName == "" || podName == "" {
		h.logger.Warnw("API: Missing required query parameters",
			"service_name", serviceName,
			"pod_name", podName,
		)
		http.Error(w, "Missing service_name or pod_name query parameters", http.StatusBadRequest)
		return
	}

	// Get service from registry
	serviceKey := serviceName + ":" + podName
	service, exists := h.registry.Get(serviceKey)
	if !exists {
		h.logger.Warnw("API: Service not found for heartbeat",
			"service_name", serviceName,
			"pod_name", podName,
		)
		http.Error(w, "Service not registered", http.StatusNotFound)
		return
	}

	h.logger.Infow("API: Heartbeat received",
		"service_name", serviceName,
		"pod_name", podName,
		"current_status", string(service.Status),
	)

	// Return success response with current timestamp
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now(),
	})
}

// validateRegistration validates a service registration
func (h *Handler) validateRegistration(reg *models.ServiceRegistration) error {
	if reg.ServiceName == "" {
		return &ValidationError{Message: "service_name is required"}
	}
	if reg.PodName == "" {
		return &ValidationError{Message: "pod_name is required"}
	}
	if len(reg.Providers) == 0 {
		return &ValidationError{Message: "at least one provider is required"}
	}
	if reg.HealthCheckURL == "" {
		return &ValidationError{Message: "health_check_url is required"}
	}
	if reg.NotificationURL == "" {
		return &ValidationError{Message: "notification_url is required"}
	}

	// Validate providers
	for i, provider := range reg.Providers {
		if provider.Protocol == "" {
			return &ValidationError{Message: "provider protocol is required", Index: &i}
		}
		if provider.IP == "" {
			return &ValidationError{Message: "provider IP is required", Index: &i}
		}
		if provider.Port <= 0 || provider.Port > 65535 {
			return &ValidationError{Message: "provider port must be between 1 and 65535", Index: &i}
		}
	}

	return nil
}

// ValidationError represents a validation error
type ValidationError struct {
	Message string
	Index   *int
}

func (e *ValidationError) Error() string {
	if e.Index != nil {
		return e.Message + " (provider index: " + string(rune(*e.Index)) + ")"
	}
	return e.Message
}
