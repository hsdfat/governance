package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/hsdfat/go-zlog/logger"
	"go.uber.org/zap"
)

// Client is a helper for services to interact with the governance manager
type Client struct {
	managerURL  string
	httpClient  *http.Client
	serviceName string
	podName     string
	logger      logger.LoggerI

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
	NotifyFunc        func(payload *models.NotificationPayload)
}

// ClientConfig contains configuration for the client
type ClientConfig struct {
	ManagerURL        string         // Manager URL (e.g., "http://manager:8080")
	ServiceName       string         // This service's name
	PodName           string         // This pod's name
	Timeout           time.Duration  // HTTP request timeout
	HeartbeatInterval time.Duration  // Interval between heartbeats (default: 30s)
	HeartbeatTimeout  time.Duration  // Time before considering manager unreachable (default: 90s)
	Logger            logger.LoggerI // Optional logger (default: uses stdlib log)
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

	// Use provided logger or create a default one
	clientLogger := config.Logger
	if clientLogger == nil {
		clientLogger = &logger.Logger{SugaredLogger: logger.NewLogger().WithOptions(zap.AddCallerSkip(1))}
	}

	return &Client{
		managerURL: config.ManagerURL,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
		serviceName:        config.ServiceName,
		podName:            config.PodName,
		logger:             clientLogger,
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
	if c.NotifyFunc != nil {
		// Call the registration callback with initial pod info
		for pod, v := range regResp.SubscribedServices {
			go c.NotifyFunc(&models.NotificationPayload{
				ServiceName: pod,
				EventType:   "initial_registration",
				Pods:        v,
				Timestamp:   time.Now(),
			})
		}

	}

	c.logger.Infow("Successfully registered",
		"service", registration.ServiceName,
		"pod", registration.PodName,

		"total_pods", len(regResp.Pods),
		"subscribed_services", len(regResp.SubscribedServices))

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

	c.logger.Infow("Successfully unregistered", "service", serviceName, "pod", podName)
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
		c.logger.Debugw("Updated own pod info", "service", serviceName, "pods", len(pods))
	} else {
		// Update subscribed service pods
		c.subscribedServices[serviceName] = make([]models.PodInfo, len(pods))
		copy(c.subscribedServices[serviceName], pods)
		c.logger.Debugw("Updated subscribed service pod info", "service", serviceName, "pods", len(pods))
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

	c.logger.Debugw("Heartbeat sent successfully", "service", c.serviceName, "pod", c.podName)
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

// StartHTTPServerWithClient starts an HTTP server with all handlers including subscribed services
// This is a convenience method that automatically includes the subscribed services endpoint
func (c *Client) StartHTTPServerWithClient(config HTTPServerConfig) error {
	if config.NotificationURL == "" {
		config.NotificationURL = "/notify"
	}
	if config.HeartbeatURL == "" {
		config.HeartbeatURL = "/heartbeat"
	}
	if config.SubscribedServicesURL == "" {
		config.SubscribedServicesURL = "/subscribed-services"
	}

	mux := http.NewServeMux()

	// Register notification handler
	mux.HandleFunc(config.NotificationURL, c.CreateNotificationHandler())

	// Register heartbeat handler
	mux.HandleFunc(config.HeartbeatURL, c.CreateHeartbeatHandler(config.HeartbeatHandler))

	// Register subscribed services handler (this is the key addition)
	mux.HandleFunc(config.SubscribedServicesURL, c.CreateSubscribedServicesHandler())

	// Register health check
	mux.HandleFunc("/health", c.CreateHealthCheckHandler())

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.Port),
		Handler: mux,
	}

	c.logger.Infow("Starting HTTP server",
		"port", config.Port,
		"notification_url", config.NotificationURL,
		"heartbeat_url", config.HeartbeatURL,
		"subscribed_services_url", config.SubscribedServicesURL,
		"health_url", "/health")
	return server.ListenAndServe()
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
				c.logger.Warnw("Heartbeat timeout exceeded, re-registering",
					"time_since_last", timeSinceLastHeartbeat,
					"timeout", c.heartbeatTimeout,
					"service", c.serviceName,
					"pod", c.podName)

				if reg != nil {
					if _, err := c.Register(reg); err != nil {
						c.logger.Errorw("Failed to re-register after heartbeat timeout", "error", err)
					} else {
						c.logger.Infow("Successfully re-registered after heartbeat timeout")
					}
				} else {
					c.logger.Warnw("Cannot re-register: no registration data stored")
				}
			} else {
				// Send heartbeat
				if err := c.SendHeartbeat(); err != nil {
					c.logger.Errorw("Failed to send heartbeat", "error", err)
				}
			}

		case <-c.stopHeartbeat:
			c.logger.Infow("Stopping heartbeat goroutine", "service", c.serviceName, "pod", c.podName)
			return
		}
	}
}

// HTTPServerConfig contains configuration for the HTTP server
type HTTPServerConfig struct {
	Port                  int                 // Port to listen on
	NotificationURL       string              // Path for notification endpoint (default: "/notify")
	HeartbeatURL          string              // Path for heartbeat endpoint (default: "/heartbeat")
	SubscribedServicesURL string              // Path for subscribed services endpoint (default: "/subscribed-services")
	NotificationHandler   NotificationHandler // Handler for notifications
	HeartbeatHandler      func() error        // Optional custom heartbeat handler
}

// CreateNotificationHandler creates an HTTP handler function for receiving notifications
// This can be integrated into existing HTTP servers (gin, chi, stdlib, etc.)
func (c *Client) CreateNotificationHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			clientIP = forwarded
		}

		if r.Method != http.MethodPost {
			c.logger.Warnw("Method not allowed", "handler", "notification", "method", r.Method, "client", clientIP)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse notification payload
		var payload models.NotificationPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			c.logger.Errorw("Failed to decode notification", "error", err, "client", clientIP)
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		c.logger.Infow("Received notification",
			"service", payload.ServiceName,
			"event", payload.EventType,
			"pods", len(payload.Pods),
			"client", clientIP,
			"timestamp", payload.Timestamp.Format(time.RFC3339))

		// Log detailed pod information at debug level
		for i, pod := range payload.Pods {
			c.logger.Debugw("Pod info",
				"index", i,
				"name", pod.PodName,
				"status", pod.Status,
				"providers", len(pod.Providers))

		}
		c.mu.Lock()
		// Update stored pod info
		c.subscribedServices[payload.ServiceName] = payload.Pods
		c.mu.Unlock()
		// Call handler asynchronously
		if c.NotifyFunc != nil {
			go c.NotifyFunc(&payload)
		}

		// Return success
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
		c.logger.Debugw("Response sent", "handler", "notification", "status", 200, "client", clientIP)
	}
}

// CreateHeartbeatHandler creates an HTTP handler function for receiving heartbeat requests
// This can be integrated into existing HTTP servers (gin, chi, stdlib, etc.)
func (c *Client) CreateHeartbeatHandler(handler func() error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			clientIP = forwarded
		}

		if r.Method != http.MethodPost && r.Method != http.MethodGet {
			c.logger.Warnw("Method not allowed", "handler", "heartbeat", "method", r.Method, "client", clientIP)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		c.logger.Debugw("Heartbeat received",
			"method", r.Method,
			"client", clientIP,
			"timestamp", time.Now().Format(time.RFC3339))

		// Call custom handler if provided
		if handler != nil {
			if err := handler(); err != nil {
				c.logger.Errorw("Custom heartbeat handler failed", "error", err, "client", clientIP)
				http.Error(w, "Heartbeat handler error", http.StatusInternalServerError)
				return
			}
			c.logger.Debugw("Custom heartbeat handler executed successfully", "client", clientIP)
		}

		// Return success
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "ok",
			"timestamp": time.Now().Format(time.RFC3339),
		})
		c.logger.Debugw("Response sent", "handler", "heartbeat", "status", 200, "client", clientIP)
	}
}

// CreateHealthCheckHandler creates an HTTP handler function for health checks
// This can be integrated into existing HTTP servers (gin, chi, stdlib, etc.)
func (c *Client) CreateHealthCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		clientIP := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			clientIP = forwarded
		}

		c.logger.Debugw("Health check requested", "method", r.Method, "client", clientIP)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status":    "healthy",
			"timestamp": time.Now().Format(time.RFC3339),
		})
	}
}

// SubscribedServicesResponse represents the response from the subscribed services endpoint
type SubscribedServicesResponse struct {
	ServiceName        string                      `json:"service_name"`
	PodName            string                      `json:"pod_name"`
	SubscribedServices map[string][]models.PodInfo `json:"subscribed_services"`
	Timestamp          string                      `json:"timestamp"`
}

// CreateSubscribedServicesHandler creates an HTTP handler function for listing subscribed services and their pods
// This can be integrated into existing HTTP servers (gin, chi, stdlib, etc.)
func (c *Client) CreateSubscribedServicesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		clientIP := r.RemoteAddr
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			clientIP = forwarded
		}

		if r.Method != http.MethodGet {
			c.logger.Warnw("Method not allowed", "handler", "subscribed_services", "method", r.Method, "client", clientIP)
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		serviceName := r.URL.Query().Get("service-name")
		provideId := r.URL.Query().Get("provide-id")
		if serviceName != "" && provideId != "" {
			c.logger.Infow("query for subscribed pod", "service-name", serviceName, "provide-id", provideId)

			response := c.GetPodInfos(serviceName, provideId)
			if len(response) == 0 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if err := json.NewEncoder(w).Encode(response); err != nil {
				c.logger.Errorw("Failed to encode subscribed services response", "error", err, "client", clientIP)
				return
			}

			c.logger.Debugw("Response sent", "handler", "subscribed_services", "services", len(response), "client", clientIP)
			return
		}

		c.logger.Debugw("Subscribed services requested", "method", r.Method, "client", clientIP)

		// Get all subscribed services with their pods
		subscribedServices := c.GetAllSubscribedServices()

		response := SubscribedServicesResponse{
			ServiceName:        c.serviceName,
			PodName:            c.podName,
			SubscribedServices: subscribedServices,
			Timestamp:          time.Now().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(response); err != nil {
			c.logger.Errorw("Failed to encode subscribed services response", "error", err, "client", clientIP)
			return
		}

		c.logger.Debugw("Response sent", "handler", "subscribed_services", "services", len(subscribedServices), "client", clientIP)
	}
}

type Pod struct {
	Name string
	Port uint16
	Ip   string
}

func (c *Client) GetPodInfos(serviceName, provideId string) (pods map[string]Pod) {
	podInfos, ok := c.subscribedServices[serviceName]
	if !ok {
		return nil
	}
	pods = make(map[string]Pod)

	for _, pod := range podInfos {
		providers := pod.Providers
		for _, provide := range providers {
			if provide.ProviderID == provideId {
				pods[pod.PodName] = Pod{
					Name: pod.PodName,
					Ip:   provide.IP,
					Port: uint16(provide.Port),
				}
			}
		}
	}

	return
}

func GetPodInfos(payload models.NotificationPayload, serviceName, provideId string) (pods map[string]Pod, err error) {
	if payload.ServiceName != serviceName {
		return nil, fmt.Errorf("service name mismatch: expected %s, got %s", serviceName, payload.ServiceName)
	}
	podInfos := payload.Pods
	pods = make(map[string]Pod)

	for _, pod := range podInfos {
		providers := pod.Providers
		for _, provide := range providers {
			if provide.ProviderID == provideId {
				pods[pod.PodName] = Pod{
					Name: pod.PodName,
					Ip:   provide.IP,
					Port: uint16(provide.Port),
				}
			}
		}
	}

	return pods, nil
}

// Event type constants
const (
	EventTypeCreate = "create"
	EventTypeUpdate = "update"
	EventTypeDelete = "delete"
)

type PodEvent struct {
	Event string // create/update/delete
	Name  string
}

func CheckDiff(old, new map[string]Pod) (created, updated, deleted []PodEvent) {
	created = make([]PodEvent, 0)
	updated = make([]PodEvent, 0)
	deleted = make([]PodEvent, 0)

	// Handle nil maps
	if old == nil {
		old = make(map[string]Pod)
	}
	if new == nil {
		new = make(map[string]Pod)
	}

	// Check for created and updated pods
	for name, newPod := range new {
		if oldPod, exists := old[name]; exists {
			// Pod exists in both - check if it was updated
			if oldPod.Ip != newPod.Ip || oldPod.Port != newPod.Port {
				updated = append(updated, PodEvent{
					Event: EventTypeUpdate,
					Name:  name,
				})
			}
		} else {
			// Pod exists in new but not in old - created
			created = append(created, PodEvent{
				Event: EventTypeCreate,
				Name:  name,
			})
		}
	}

	// Check for deleted pods
	for name := range old {
		if _, exists := new[name]; !exists {
			// Pod exists in old but not in new - deleted
			deleted = append(deleted, PodEvent{
				Event: EventTypeDelete,
				Name:  name,
			})
		}
	}

	return
}
