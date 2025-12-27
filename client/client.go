package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/chronnie/governance/models"
)

// Client is a helper for services to interact with the governance manager
type Client struct {
	managerURL  string
	httpClient  *http.Client
	serviceName string
	podName     string

	// Pod info storage
	mu                 sync.RWMutex
	ownPods            []models.PodInfo            // Pods of this service
	subscribedServices map[string][]models.PodInfo // Pods of subscribed services

	// Heartbeat tracking
	lastHeartbeatTime time.Time
	heartbeatInterval time.Duration
	heartbeatTimeout  time.Duration
	stopHeartbeat     chan struct{}
	heartbeatStopped  chan struct{}
	registration      *models.ServiceRegistration
}

// ClientConfig contains configuration for the client
type ClientConfig struct {
	ManagerURL        string        // Manager URL (e.g., "http://manager:8080")
	ServiceName       string        // This service's name
	PodName           string        // This pod's name
	Timeout           time.Duration // HTTP request timeout
	HeartbeatInterval time.Duration // Interval between heartbeats (default: 30s)
	HeartbeatTimeout  time.Duration // Time before considering manager unreachable (default: 90s)
}

// NewClient creates a new governance client
func NewClient(config *ClientConfig) *Client {
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Second
	}
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	if config.HeartbeatTimeout == 0 {
		config.HeartbeatTimeout = 90 * time.Second
	}

	return &Client{
		managerURL: config.ManagerURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		serviceName:        config.ServiceName,
		podName:            config.PodName,
		ownPods:            make([]models.PodInfo, 0),
		subscribedServices: make(map[string][]models.PodInfo),
		heartbeatInterval:  config.HeartbeatInterval,
		heartbeatTimeout:   config.HeartbeatTimeout,
		stopHeartbeat:      make(chan struct{}),
		heartbeatStopped:   make(chan struct{}),
	}
}

// Register registers this service with the manager and returns the pod info list
func (c *Client) Register(registration *models.ServiceRegistration) (*models.RegistrationResponse, error) {
	// Set service name and pod name if not already set
	if registration.ServiceName == "" {
		registration.ServiceName = c.serviceName
	} else {
		c.serviceName = registration.ServiceName
	}
	if registration.PodName == "" {
		registration.PodName = c.podName
	} else {
		c.podName = registration.PodName
	}

	// Marshal registration to JSON
	jsonData, err := json.Marshal(registration)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal registration: %w", err)
	}

	// Create HTTP request
	url := c.managerURL + "/register"
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("failed to create register request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send register request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Try to parse error response
		var errResp models.ErrorResponse
		if json.Unmarshal(body, &errResp) == nil && errResp.Error != "" {
			return nil, fmt.Errorf("register failed: %s - %s", errResp.Error, errResp.Message)
		}
		return nil, fmt.Errorf("register request failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Parse successful response
	var regResp models.RegistrationResponse
	if err := json.Unmarshal(body, &regResp); err != nil {
		return nil, fmt.Errorf("failed to parse registration response: %w", err)
	}

	// Store pod info in client
	c.mu.Lock()
	c.ownPods = regResp.Pods
	c.subscribedServices = regResp.SubscribedServices
	if c.subscribedServices == nil {
		c.subscribedServices = make(map[string][]models.PodInfo)
	}
	c.registration = registration
	c.lastHeartbeatTime = time.Now()
	c.mu.Unlock()

	log.Printf("[Client] Successfully registered: service=%s, pod=%s, total_pods=%d, subscribed_services=%d",
		registration.ServiceName, registration.PodName, len(regResp.Pods), len(regResp.SubscribedServices))

	return &regResp, nil
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

// GetOwnPods returns the current list of pods for this service
func (c *Client) GetOwnPods() []models.PodInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to prevent external modification
	pods := make([]models.PodInfo, len(c.ownPods))
	copy(pods, c.ownPods)
	return pods
}

// GetSubscribedServicePods returns the pods for a specific subscribed service
func (c *Client) GetSubscribedServicePods(serviceName string) ([]models.PodInfo, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pods, exists := c.subscribedServices[serviceName]
	if !exists {
		return nil, false
	}

	// Return a copy to prevent external modification
	podsCopy := make([]models.PodInfo, len(pods))
	copy(podsCopy, pods)
	return podsCopy, true
}

// GetAllSubscribedServices returns all subscribed services and their pods
func (c *Client) GetAllSubscribedServices() map[string][]models.PodInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a deep copy to prevent external modification
	result := make(map[string][]models.PodInfo, len(c.subscribedServices))
	for serviceName, pods := range c.subscribedServices {
		podsCopy := make([]models.PodInfo, len(pods))
		copy(podsCopy, pods)
		result[serviceName] = podsCopy
	}
	return result
}

// ProviderEndpoint represents a specific provider endpoint
type ProviderEndpoint struct {
	PodName    string
	ProviderID string
	Protocol   models.Protocol
	IP         string
	Port       int
	Status     models.ServiceStatus
}

// GetProviderEndpoints returns all endpoints for a specific provider ID from a subscribed service
func (c *Client) GetProviderEndpoints(serviceName, providerID string) []ProviderEndpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pods, exists := c.subscribedServices[serviceName]
	if !exists {
		return []ProviderEndpoint{}
	}

	endpoints := make([]ProviderEndpoint, 0)
	for _, pod := range pods {
		for _, provider := range pod.Providers {
			if provider.ProviderID == providerID {
				endpoints = append(endpoints, ProviderEndpoint{
					PodName:    pod.PodName,
					ProviderID: provider.ProviderID,
					Protocol:   provider.Protocol,
					IP:         provider.IP,
					Port:       provider.Port,
					Status:     pod.Status,
				})
			}
		}
	}

	return endpoints
}

// GetAllProviderEndpoints returns all endpoints for all provider IDs from a subscribed service
func (c *Client) GetAllProviderEndpoints(serviceName string) map[string][]ProviderEndpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	pods, exists := c.subscribedServices[serviceName]
	if !exists {
		return make(map[string][]ProviderEndpoint)
	}

	result := make(map[string][]ProviderEndpoint)
	for _, pod := range pods {
		for _, provider := range pod.Providers {
			endpoint := ProviderEndpoint{
				PodName:    pod.PodName,
				ProviderID: provider.ProviderID,
				Protocol:   provider.Protocol,
				IP:         provider.IP,
				Port:       provider.Port,
				Status:     pod.Status,
			}
			result[provider.ProviderID] = append(result[provider.ProviderID], endpoint)
		}
	}

	return result
}

// UpdatePodInfo updates the stored pod info (called internally when notifications are received)
func (c *Client) UpdatePodInfo(serviceName string, pods []models.PodInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if serviceName == c.serviceName {
		// Update own pods
		c.ownPods = make([]models.PodInfo, len(pods))
		copy(c.ownPods, pods)
		log.Printf("[Client] Updated own pod info: service=%s, pods=%d", serviceName, len(pods))
	} else {
		// Update subscribed service pods
		c.subscribedServices[serviceName] = make([]models.PodInfo, len(pods))
		copy(c.subscribedServices[serviceName], pods)
		log.Printf("[Client] Updated subscribed service pod info: service=%s, pods=%d", serviceName, len(pods))
	}
}

// NotificationHandler is a function type for handling notifications
type NotificationHandler func(payload *models.NotificationPayload)

// WrapNotificationHandler wraps a user's notification handler to automatically update
// the client's stored pod info when notifications are received
func (c *Client) WrapNotificationHandler(userHandler NotificationHandler) NotificationHandler {
	return func(payload *models.NotificationPayload) {
		// First, update the stored pod info
		c.UpdatePodInfo(payload.ServiceName, payload.Pods)

		// Then call the user's handler
		if userHandler != nil {
			userHandler(payload)
		}
	}
}

// SendHeartbeat sends a heartbeat to the manager and updates the last heartbeat time
func (c *Client) SendHeartbeat() error {
	url := fmt.Sprintf("%s/heartbeat?service_name=%s&pod_name=%s", c.managerURL, c.serviceName, c.podName)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create heartbeat request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send heartbeat: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("heartbeat failed with status %d: %s", resp.StatusCode, string(body))
	}

	// Update last heartbeat time
	c.mu.Lock()
	c.lastHeartbeatTime = time.Now()
	c.mu.Unlock()

	log.Printf("[Client] Heartbeat sent successfully: service=%s, pod=%s", c.serviceName, c.podName)
	return nil
}

// StartHeartbeat starts the background heartbeat goroutine
func (c *Client) StartHeartbeat() {
	go c.heartbeatLoop()
}

// StopHeartbeat stops the background heartbeat goroutine
func (c *Client) StopHeartbeat() {
	close(c.stopHeartbeat)
	<-c.heartbeatStopped
}

// heartbeatLoop runs in the background and sends heartbeats periodically
func (c *Client) heartbeatLoop() {
	ticker := time.NewTicker(c.heartbeatInterval)
	defer ticker.Stop()
	defer close(c.heartbeatStopped)

	for {
		select {
		case <-ticker.C:
			// Check if we've exceeded the heartbeat timeout
			c.mu.RLock()
			timeSinceLastHeartbeat := time.Since(c.lastHeartbeatTime)
			reg := c.registration
			c.mu.RUnlock()

			if timeSinceLastHeartbeat > c.heartbeatTimeout {
				log.Printf("[Client] Heartbeat timeout exceeded (%v > %v), re-registering: service=%s, pod=%s",
					timeSinceLastHeartbeat, c.heartbeatTimeout, c.serviceName, c.podName)

				if reg != nil {
					if _, err := c.Register(reg); err != nil {
						log.Printf("[Client] Failed to re-register after heartbeat timeout: %v", err)
					} else {
						log.Printf("[Client] Successfully re-registered after heartbeat timeout")
					}
				} else {
					log.Printf("[Client] Cannot re-register: no registration data stored")
				}
			} else {
				// Send heartbeat
				if err := c.SendHeartbeat(); err != nil {
					log.Printf("[Client] Failed to send heartbeat: %v", err)
				}
			}

		case <-c.stopHeartbeat:
			log.Printf("[Client] Stopping heartbeat goroutine: service=%s, pod=%s", c.serviceName, c.podName)
			return
		}
	}
}

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
