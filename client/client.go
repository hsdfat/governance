package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/chronnie/governance/models"
)

// Client is a helper for services to interact with the governance manager
type Client struct {
	managerURL  string
	httpClient  *http.Client
	serviceName string
	podName     string
}

// ClientConfig contains configuration for the client
type ClientConfig struct {
	ManagerURL  string        // Manager URL (e.g., "http://manager:8080")
	ServiceName string        // This service's name
	PodName     string        // This pod's name
	Timeout     time.Duration // HTTP request timeout
}

// NewClient creates a new governance client
func NewClient(config *ClientConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}

	return &Client{
		managerURL: config.ManagerURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		serviceName: config.ServiceName,
		podName:     config.PodName,
	}
}

// Register registers this service with the manager
func (c *Client) Register(registration *models.ServiceRegistration) error {
	// Set service name and pod name if not already set
	if registration.ServiceName == "" {
		registration.ServiceName = c.serviceName
	}
	if registration.PodName == "" {
		registration.PodName = c.podName
	}

	// Marshal registration to JSON
	jsonData, err := json.Marshal(registration)
	if err != nil {
		return fmt.Errorf("failed to marshal registration: %w", err)
	}

	// Create HTTP request
	url := c.managerURL + "/register"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create register request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send register request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("register request failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[Client] Successfully registered: service=%s, pod=%s", registration.ServiceName, registration.PodName)
	return nil
}

// Unregister unregisters this service from the manager
func (c *Client) Unregister() error {
	return c.UnregisterService(c.serviceName, c.podName)
}

// UnregisterService unregisters a specific service/pod from the manager
func (c *Client) UnregisterService(serviceName, podName string) error {
	// Create HTTP request
	url := fmt.Sprintf("%s/unregister?service_name=%s&pod_name=%s", c.managerURL, serviceName, podName)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create unregister request: %w", err)
	}

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send unregister request: %w", err)
	}
	defer resp.Body.Close()

	// Check response
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("unregister request failed with status %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("[Client] Successfully unregistered: service=%s, pod=%s", serviceName, podName)
	return nil
}

// NotificationHandler is a function type for handling notifications
type NotificationHandler func(payload *models.NotificationPayload)

// NotificationServer helps services receive notifications from the manager
type NotificationServer struct {
	port    int
	handler NotificationHandler
	server  *http.Server
}

// NewNotificationServer creates a new notification server
func NewNotificationServer(port int, handler NotificationHandler) *NotificationServer {
	ns := &NotificationServer{
		port:    port,
		handler: handler,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/notify", ns.handleNotification)
	mux.HandleFunc("/health", ns.handleHealth)

	ns.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return ns
}

// Start starts the notification server
func (ns *NotificationServer) Start() error {
	log.Printf("[NotificationServer] Starting on port %d", ns.port)
	return ns.server.ListenAndServe()
}

// Stop gracefully stops the notification server
func (ns *NotificationServer) Stop(ctx context.Context) error {
	log.Println("[NotificationServer] Stopping...")
	return ns.server.Shutdown(ctx)
}

// handleNotification handles incoming notification requests
func (ns *NotificationServer) handleNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse notification payload
	var payload models.NotificationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Printf("[NotificationServer] Failed to decode notification: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	log.Printf("[NotificationServer] Received notification: service=%s, event=%s, pods=%d",
		payload.ServiceName, payload.EventType, len(payload.Pods))

	// Call handler
	if ns.handler != nil {
		go ns.handler(&payload)
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

// handleHealth handles health check requests
func (ns *NotificationServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "healthy",
	})
}

// GetNotificationURL returns the notification URL for this server
func (ns *NotificationServer) GetNotificationURL(host string) string {
	return fmt.Sprintf("http://%s:%d/notify", host, ns.port)
}

// GetHealthCheckURL returns the health check URL for this server
func (ns *NotificationServer) GetHealthCheckURL(host string) string {
	return fmt.Sprintf("http://%s:%d/health", host, ns.port)
}
