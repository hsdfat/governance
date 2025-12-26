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
	"go.uber.org/zap"
)

// Handler handles HTTP requests for the governance manager
type Handler struct {
	registry   *registry.Registry
	eventQueue eventqueue.IEventQueue
}

// NewHandler creates a new API handler
func NewHandler(reg *registry.Registry, eventQueue eventqueue.IEventQueue) *Handler {
	return &Handler{
		registry:   reg,
		eventQueue: eventQueue,
	}
}

// RegisterHandler handles POST /register requests
func (h *Handler) RegisterHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("API: Received register request",
		zap.String("method", r.Method),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if r.Method != http.MethodPost {
		logger.Warn("API: Invalid method for register endpoint",
			zap.String("method", r.Method),
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var registration models.ServiceRegistration
	if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
		logger.Error("API: Failed to decode registration request",
			zap.Error(err),
			zap.String("remote_addr", r.RemoteAddr),
		)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logger.Debug("API: Parsed registration request",
		zap.String("service_name", registration.ServiceName),
		zap.String("pod_name", registration.PodName),
		zap.Int("providers_count", len(registration.Providers)),
		zap.Int("subscriptions_count", len(registration.Subscriptions)),
	)

	// Validate registration
	if err := h.validateRegistration(&registration); err != nil {
		logger.Warn("API: Invalid registration request",
			zap.String("service_name", registration.ServiceName),
			zap.String("pod_name", registration.PodName),
			zap.Error(err),
		)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	logger.Info("API: Registration validated successfully",
		zap.String("service_name", registration.ServiceName),
		zap.String("pod_name", registration.PodName),
	)

	// Create result channel to receive pod list
	resultChan := make(chan *events.RegisterResult, 1)

	// Create context with event data and result channel
	ctx := events.NewRegisterContextWithResult(&registration, resultChan)

	// Create and enqueue register event (with deadline for register events)
	event := eventqueue.NewEvent(string(events.EventRegister), ctx, eventqueue.WithTimeout(5*time.Second))

	if err := h.eventQueue.Enqueue(event); err != nil {
		logger.Error("API: Failed to enqueue register event",
			zap.String("service_name", registration.ServiceName),
			zap.String("pod_name", registration.PodName),
			zap.Error(err),
		)
		http.Error(w, "Failed to process registration", http.StatusInternalServerError)
		return
	}

	logger.Info("API: Register event enqueued successfully, waiting for result",
		zap.String("service_name", registration.ServiceName),
		zap.String("pod_name", registration.PodName),
	)

	// Wait for result with timeout
	select {
	case result := <-resultChan:
		if result.Error != nil {
			logger.Error("API: Registration processing failed",
				zap.String("service_name", registration.ServiceName),
				zap.String("pod_name", registration.PodName),
				zap.Error(result.Error),
			)
			http.Error(w, result.Error.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("API: Registration completed successfully",
			zap.String("service_name", registration.ServiceName),
			zap.String("pod_name", registration.PodName),
			zap.Int("pod_count", len(result.Pods)),
			zap.Int("subscribed_services_count", len(result.SubscribedServices)),
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

		logger.Debug("API: Sent success response with pod info and subscribed services",
			zap.String("service_name", registration.ServiceName),
			zap.String("pod_name", registration.PodName),
			zap.Int("pod_count", len(result.Pods)),
			zap.Int("subscribed_services_count", len(result.SubscribedServices)),
		)

	case <-time.After(10 * time.Second):
		logger.Error("API: Registration processing timeout",
			zap.String("service_name", registration.ServiceName),
			zap.String("pod_name", registration.PodName),
		)
		http.Error(w, "Registration processing timeout", http.StatusGatewayTimeout)
	}
}

// UnregisterHandler handles DELETE /unregister requests
func (h *Handler) UnregisterHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("API: Received unregister request",
		zap.String("method", r.Method),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if r.Method != http.MethodDelete {
		logger.Warn("API: Invalid method for unregister endpoint",
			zap.String("method", r.Method),
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	serviceName := r.URL.Query().Get("service_name")
	podName := r.URL.Query().Get("pod_name")

	if serviceName == "" || podName == "" {
		logger.Warn("API: Missing required query parameters",
			zap.String("service_name", serviceName),
			zap.String("pod_name", podName),
		)
		http.Error(w, "Missing service_name or pod_name query parameters", http.StatusBadRequest)
		return
	}

	logger.Info("API: Unregister request validated",
		zap.String("service_name", serviceName),
		zap.String("pod_name", podName),
	)

	// Create context with event data
	ctx := events.NewUnregisterContext(serviceName, podName)

	// Create and enqueue unregister event (with deadline for unregister events)
	event := eventqueue.NewEvent(string(events.EventUnregister), ctx, eventqueue.WithTimeout(5*time.Second))

	if err := h.eventQueue.Enqueue(event); err != nil {
		logger.Error("API: Failed to enqueue unregister event",
			zap.String("service_name", serviceName),
			zap.String("pod_name", podName),
			zap.Error(err),
		)
		http.Error(w, "Failed to process unregistration", http.StatusInternalServerError)
		return
	}

	logger.Info("API: Unregister event enqueued successfully",
		zap.String("service_name", serviceName),
		zap.String("pod_name", podName),
	)

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Unregistration event queued successfully",
	})

	logger.Debug("API: Sent success response for unregistration",
		zap.String("service_name", serviceName),
		zap.String("pod_name", podName),
	)
}

// ServicesHandler handles GET /services requests (for debugging)
func (h *Handler) ServicesHandler(w http.ResponseWriter, r *http.Request) {
	logger.Debug("API: Received services query request",
		zap.String("method", r.Method),
		zap.String("remote_addr", r.RemoteAddr),
	)

	if r.Method != http.MethodGet {
		logger.Warn("API: Invalid method for services endpoint",
			zap.String("method", r.Method),
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	services := h.registry.GetAllServices()

	logger.Info("API: Retrieved all services",
		zap.Int("service_count", len(services)),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":    len(services),
		"services": services,
	})

	logger.Debug("API: Sent services response",
		zap.Int("service_count", len(services)),
	)
}

// HealthHandler handles GET /health requests
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	logger.Debug("API: Received health check request",
		zap.String("remote_addr", r.RemoteAddr),
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
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
