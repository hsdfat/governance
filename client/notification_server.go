package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/chronnie/governance/models"
)

// NotificationServer is a simple HTTP server for receiving notifications and health checks
type NotificationServer struct {
	port    int
	handler NotificationHandler
	server  *http.Server
}

// NewNotificationServer creates a new notification server
func NewNotificationServer(port int, handler NotificationHandler) *NotificationServer {
	return &NotificationServer{
		port:    port,
		handler: handler,
	}
}

// Start starts the notification server
func (ns *NotificationServer) Start() error {
	mux := http.NewServeMux()

	// Notification endpoint
	mux.HandleFunc("/notify", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload models.NotificationPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Call handler asynchronously
		if ns.handler != nil {
			go ns.handler(&payload)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "healthy",
		})
	})

	ns.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", ns.port),
		Handler: mux,
	}

	return ns.server.ListenAndServe()
}

// Stop stops the notification server
func (ns *NotificationServer) Stop(ctx context.Context) error {
	if ns.server != nil {
		return ns.server.Shutdown(ctx)
	}
	return nil
}

// GetHealthCheckURL returns the health check URL for this server
func (ns *NotificationServer) GetHealthCheckURL(ip string) string {
	return fmt.Sprintf("http://%s:%d/health", ip, ns.port)
}

// GetNotificationURL returns the notification URL for this server
func (ns *NotificationServer) GetNotificationURL(ip string) string {
	return fmt.Sprintf("http://%s:%d/notify", ip, ns.port)
}
