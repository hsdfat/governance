package api

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/events"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/models"
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
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var registration models.ServiceRegistration
	if err := json.NewDecoder(r.Body).Decode(&registration); err != nil {
		log.Printf("[API] Failed to decode registration request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate registration
	if err := h.validateRegistration(&registration); err != nil {
		log.Printf("[API] Invalid registration: %v", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	log.Printf("[API] Received registration: service=%s, pod=%s", registration.ServiceName, registration.PodName)

	// Create context with event data
	ctx := events.NewRegisterContext(&registration)

	// Create and enqueue register event (with deadline for register events)
	event := eventqueue.NewEvent(string(events.EventRegister), ctx, eventqueue.WithTimeout(5*time.Second))

	if err := h.eventQueue.Enqueue(event); err != nil {
		log.Printf("[API] Failed to enqueue register event: %v", err)
		http.Error(w, "Failed to process registration", http.StatusInternalServerError)
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Registration event queued successfully",
	})

	log.Printf("[API] Registration accepted: service=%s, pod=%s", registration.ServiceName, registration.PodName)
}

// UnregisterHandler handles DELETE /unregister requests
func (h *Handler) UnregisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	serviceName := r.URL.Query().Get("service_name")
	podName := r.URL.Query().Get("pod_name")

	if serviceName == "" || podName == "" {
		http.Error(w, "Missing service_name or pod_name query parameters", http.StatusBadRequest)
		return
	}

	log.Printf("[API] Received unregister request: service=%s, pod=%s", serviceName, podName)

	// Create context with event data
	ctx := events.NewUnregisterContext(serviceName, podName)

	// Create and enqueue unregister event (with deadline for unregister events)
	event := eventqueue.NewEvent(string(events.EventUnregister), ctx, eventqueue.WithTimeout(5*time.Second))

	if err := h.eventQueue.Enqueue(event); err != nil {
		log.Printf("[API] Failed to enqueue unregister event: %v", err)
		http.Error(w, "Failed to process unregistration", http.StatusInternalServerError)
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "accepted",
		"message": "Unregistration event queued successfully",
	})

	log.Printf("[API] Unregistration accepted: service=%s, pod=%s", serviceName, podName)
}

// ServicesHandler handles GET /services requests (for debugging)
func (h *Handler) ServicesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	services := h.registry.GetAllServices()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"count":    len(services),
		"services": services,
	})
}

// HealthHandler handles GET /health requests
func (h *Handler) HealthHandler(w http.ResponseWriter, r *http.Request) {
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
