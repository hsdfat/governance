package client

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chronnie/governance/models"
)

// Test helper to create a test client
func createTestClient(managerURL string) *Client {
	return NewClient(&ClientConfig{
		ManagerURL:  managerURL,
		ServiceName: "test-service",
		PodName:     "test-pod-1",
		Timeout:     5 * time.Second,
	})
}

// Test helper to create sample pod info
func createSamplePodInfo(podName string, status models.ServiceStatus) models.PodInfo {
	return models.PodInfo{
		PodName: podName,
		Status:  status,
		Providers: []models.ProviderInfo{
			{
				ProviderID: "test-http",
				Protocol:   models.ProtocolHTTP,
				IP:         "192.168.1.10",
				Port:       8080,
			},
		},
	}
}

func TestClient_Register_StoresPodInfo(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST request, got %s", r.Method)
		}
		if r.URL.Path != "/register" {
			t.Errorf("Expected path /register, got %s", r.URL.Path)
		}

		// Send response with pod info
		response := models.RegistrationResponse{
			Status:      "success",
			Message:     "Registration completed successfully",
			ServiceName: "test-service",
			Pods: []models.PodInfo{
				createSamplePodInfo("test-pod-1", models.StatusHealthy),
				createSamplePodInfo("test-pod-2", models.StatusHealthy),
			},
			SubscribedServices: map[string][]models.PodInfo{
				"order-service": {
					createSamplePodInfo("order-pod-1", models.StatusHealthy),
				},
				"payment-service": {
					createSamplePodInfo("payment-pod-1", models.StatusHealthy),
					createSamplePodInfo("payment-pod-2", models.StatusUnhealthy),
				},
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	// Create client
	client := createTestClient(server.URL)

	// Register
	registration := &models.ServiceRegistration{
		ServiceName: "test-service",
		PodName:     "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: "test-http", Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions: []models.Subscription{
			{ServiceName: "order-service"},
			{ServiceName: "payment-service"},
		},
	}

	resp, err := client.Register(registration)
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	// Verify response
	if resp.Status != "success" {
		t.Errorf("Expected status 'success', got '%s'", resp.Status)
	}
	if len(resp.Pods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(resp.Pods))
	}
	if len(resp.SubscribedServices) != 2 {
		t.Errorf("Expected 2 subscribed services, got %d", len(resp.SubscribedServices))
	}

	// Verify stored pod info
	ownPods := client.GetOwnPods()
	if len(ownPods) != 2 {
		t.Errorf("Expected 2 own pods stored, got %d", len(ownPods))
	}

	allSubscribed := client.GetAllSubscribedServices()
	if len(allSubscribed) != 2 {
		t.Errorf("Expected 2 subscribed services stored, got %d", len(allSubscribed))
	}

	if orderPods, exists := allSubscribed["order-service"]; !exists {
		t.Error("Expected order-service to be stored")
	} else if len(orderPods) != 1 {
		t.Errorf("Expected 1 order-service pod, got %d", len(orderPods))
	}

	if paymentPods, exists := allSubscribed["payment-service"]; !exists {
		t.Error("Expected payment-service to be stored")
	} else if len(paymentPods) != 2 {
		t.Errorf("Expected 2 payment-service pods, got %d", len(paymentPods))
	}
}

func TestClient_Register_ErrorHandling(t *testing.T) {
	// Create mock server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(models.ErrorResponse{
			Error:   "validation_error",
			Message: "service_name is required",
		})
	}))
	defer server.Close()

	client := createTestClient(server.URL)

	registration := &models.ServiceRegistration{
		PodName: "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
	}

	resp, err := client.Register(registration)
	if err == nil {
		t.Fatal("Expected error, got nil")
	}
	if resp != nil {
		t.Errorf("Expected nil response, got %+v", resp)
	}

	// Verify error message contains the error details
	expectedErr := "register failed: validation_error - service_name is required"
	if err.Error() != expectedErr {
		t.Errorf("Expected error '%s', got '%s'", expectedErr, err.Error())
	}
}

func TestClient_GetOwnPods(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Initially should be empty
	pods := client.GetOwnPods()
	if len(pods) != 0 {
		t.Errorf("Expected 0 pods initially, got %d", len(pods))
	}

	// Manually set pods (simulating registration)
	client.mu.Lock()
	client.ownPods = []models.PodInfo{
		createSamplePodInfo("pod-1", models.StatusHealthy),
		createSamplePodInfo("pod-2", models.StatusHealthy),
	}
	client.mu.Unlock()

	// Get pods
	pods = client.GetOwnPods()
	if len(pods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(pods))
	}

	// Verify it returns a copy (modifying returned slice shouldn't affect internal state)
	pods[0].PodName = "modified"
	internalPods := client.GetOwnPods()
	if internalPods[0].PodName == "modified" {
		t.Error("GetOwnPods should return a copy, not the original slice")
	}
}

func TestClient_GetSubscribedServicePods(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Test non-existent service
	pods, exists := client.GetSubscribedServicePods("non-existent")
	if exists {
		t.Error("Expected service to not exist")
	}
	if pods != nil {
		t.Error("Expected nil pods for non-existent service")
	}

	// Add subscribed service
	client.mu.Lock()
	client.subscribedServices["order-service"] = []models.PodInfo{
		createSamplePodInfo("order-pod-1", models.StatusHealthy),
		createSamplePodInfo("order-pod-2", models.StatusUnhealthy),
	}
	client.mu.Unlock()

	// Get subscribed service pods
	pods, exists = client.GetSubscribedServicePods("order-service")
	if !exists {
		t.Error("Expected service to exist")
	}
	if len(pods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(pods))
	}

	// Verify it returns a copy
	pods[0].PodName = "modified"
	internalPods, _ := client.GetSubscribedServicePods("order-service")
	if internalPods[0].PodName == "modified" {
		t.Error("GetSubscribedServicePods should return a copy")
	}
}

func TestClient_GetAllSubscribedServices(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Initially should be empty
	services := client.GetAllSubscribedServices()
	if len(services) != 0 {
		t.Errorf("Expected 0 services initially, got %d", len(services))
	}

	// Add multiple subscribed services
	client.mu.Lock()
	client.subscribedServices["order-service"] = []models.PodInfo{
		createSamplePodInfo("order-pod-1", models.StatusHealthy),
	}
	client.subscribedServices["payment-service"] = []models.PodInfo{
		createSamplePodInfo("payment-pod-1", models.StatusHealthy),
		createSamplePodInfo("payment-pod-2", models.StatusHealthy),
	}
	client.mu.Unlock()

	// Get all services
	services = client.GetAllSubscribedServices()
	if len(services) != 2 {
		t.Errorf("Expected 2 services, got %d", len(services))
	}

	if orderPods, exists := services["order-service"]; !exists {
		t.Error("Expected order-service to exist")
	} else if len(orderPods) != 1 {
		t.Errorf("Expected 1 order pod, got %d", len(orderPods))
	}

	if paymentPods, exists := services["payment-service"]; !exists {
		t.Error("Expected payment-service to exist")
	} else if len(paymentPods) != 2 {
		t.Errorf("Expected 2 payment pods, got %d", len(paymentPods))
	}

	// Verify deep copy
	services["order-service"][0].PodName = "modified"
	internalServices := client.GetAllSubscribedServices()
	if internalServices["order-service"][0].PodName == "modified" {
		t.Error("GetAllSubscribedServices should return a deep copy")
	}
}

func TestClient_UpdatePodInfo_OwnService(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Update own pods
	newPods := []models.PodInfo{
		createSamplePodInfo("test-pod-1", models.StatusHealthy),
		createSamplePodInfo("test-pod-2", models.StatusHealthy),
		createSamplePodInfo("test-pod-3", models.StatusUnhealthy),
	}

	client.UpdatePodInfo("test-service", newPods)

	// Verify update
	ownPods := client.GetOwnPods()
	if len(ownPods) != 3 {
		t.Errorf("Expected 3 pods, got %d", len(ownPods))
	}
	if ownPods[0].PodName != "test-pod-1" {
		t.Errorf("Expected pod name 'test-pod-1', got '%s'", ownPods[0].PodName)
	}
	if ownPods[2].Status != models.StatusUnhealthy {
		t.Errorf("Expected pod status 'unhealthy', got '%s'", ownPods[2].Status)
	}
}

func TestClient_UpdatePodInfo_SubscribedService(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Update subscribed service
	newPods := []models.PodInfo{
		createSamplePodInfo("order-pod-1", models.StatusHealthy),
		createSamplePodInfo("order-pod-2", models.StatusHealthy),
	}

	client.UpdatePodInfo("order-service", newPods)

	// Verify update
	orderPods, exists := client.GetSubscribedServicePods("order-service")
	if !exists {
		t.Error("Expected order-service to exist after update")
	}
	if len(orderPods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(orderPods))
	}

	// Update again with different pods
	updatedPods := []models.PodInfo{
		createSamplePodInfo("order-pod-1", models.StatusUnhealthy),
	}
	client.UpdatePodInfo("order-service", updatedPods)

	// Verify second update
	orderPods, _ = client.GetSubscribedServicePods("order-service")
	if len(orderPods) != 1 {
		t.Errorf("Expected 1 pod after update, got %d", len(orderPods))
	}
	if orderPods[0].Status != models.StatusUnhealthy {
		t.Errorf("Expected status 'unhealthy', got '%s'", orderPods[0].Status)
	}
}

func TestClient_WrapNotificationHandler(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Track if user handler was called
	userHandlerCalled := false
	var receivedPayload *models.NotificationPayload

	userHandler := func(payload *models.NotificationPayload) {
		userHandlerCalled = true
		receivedPayload = payload
	}

	// Wrap the handler
	wrappedHandler := client.WrapNotificationHandler(userHandler)

	// Create notification payload
	payload := &models.NotificationPayload{
		ServiceName: "order-service",
		EventType:   models.EventTypeRegister,
		Timestamp:   time.Now(),
		Pods: []models.PodInfo{
			createSamplePodInfo("order-pod-1", models.StatusHealthy),
			createSamplePodInfo("order-pod-2", models.StatusHealthy),
		},
	}

	// Call wrapped handler
	wrappedHandler(payload)

	// Verify user handler was called
	if !userHandlerCalled {
		t.Error("Expected user handler to be called")
	}
	if receivedPayload == nil {
		t.Fatal("Expected payload to be passed to user handler")
	}
	if receivedPayload.ServiceName != "order-service" {
		t.Errorf("Expected service name 'order-service', got '%s'", receivedPayload.ServiceName)
	}

	// Verify pod info was updated
	orderPods, exists := client.GetSubscribedServicePods("order-service")
	if !exists {
		t.Error("Expected order-service pods to be stored")
	}
	if len(orderPods) != 2 {
		t.Errorf("Expected 2 pods to be stored, got %d", len(orderPods))
	}
}

func TestClient_WrapNotificationHandler_OwnService(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	wrappedHandler := client.WrapNotificationHandler(nil)

	// Notification for own service
	payload := &models.NotificationPayload{
		ServiceName: "test-service",
		EventType:   models.EventTypeUpdate,
		Timestamp:   time.Now(),
		Pods: []models.PodInfo{
			createSamplePodInfo("test-pod-1", models.StatusHealthy),
			createSamplePodInfo("test-pod-2", models.StatusUnhealthy),
		},
	}

	wrappedHandler(payload)

	// Verify own pods were updated
	ownPods := client.GetOwnPods()
	if len(ownPods) != 2 {
		t.Errorf("Expected 2 own pods to be stored, got %d", len(ownPods))
	}
	if ownPods[1].Status != models.StatusUnhealthy {
		t.Errorf("Expected second pod status 'unhealthy', got '%s'", ownPods[1].Status)
	}
}

func TestClient_WrapNotificationHandler_NilUserHandler(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Wrap with nil user handler
	wrappedHandler := client.WrapNotificationHandler(nil)

	payload := &models.NotificationPayload{
		ServiceName: "order-service",
		EventType:   models.EventTypeRegister,
		Timestamp:   time.Now(),
		Pods: []models.PodInfo{
			createSamplePodInfo("order-pod-1", models.StatusHealthy),
		},
	}

	// Should not panic even with nil handler
	wrappedHandler(payload)

	// Verify pod info was still updated
	orderPods, exists := client.GetSubscribedServicePods("order-service")
	if !exists {
		t.Error("Expected order-service pods to be stored")
	}
	if len(orderPods) != 1 {
		t.Errorf("Expected 1 pod to be stored, got %d", len(orderPods))
	}
}

func TestClient_ThreadSafety(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// Run concurrent operations
	done := make(chan bool)
	iterations := 100

	// Concurrent updates
	go func() {
		for i := 0; i < iterations; i++ {
			client.UpdatePodInfo("test-service", []models.PodInfo{
				createSamplePodInfo("pod-1", models.StatusHealthy),
			})
		}
		done <- true
	}()

	// Concurrent reads
	go func() {
		for i := 0; i < iterations; i++ {
			_ = client.GetOwnPods()
		}
		done <- true
	}()

	// Concurrent subscribed service updates
	go func() {
		for i := 0; i < iterations; i++ {
			client.UpdatePodInfo("order-service", []models.PodInfo{
				createSamplePodInfo("order-pod-1", models.StatusHealthy),
			})
		}
		done <- true
	}()

	// Concurrent subscribed service reads
	go func() {
		for i := 0; i < iterations; i++ {
			_, _ = client.GetSubscribedServicePods("order-service")
		}
		done <- true
	}()

	// Wait for all goroutines
	for i := 0; i < 4; i++ {
		<-done
	}

	// If we get here without race detector errors, the test passes
}

func TestClient_SendHeartbeat_Success(t *testing.T) {
	// Track heartbeat requests
	heartbeatReceived := false

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/heartbeat" {
			heartbeatReceived = true
			if r.Method != http.MethodPost {
				t.Errorf("Expected POST request, got %s", r.Method)
			}

			serviceName := r.URL.Query().Get("service_name")
			podName := r.URL.Query().Get("pod_name")

			if serviceName != "test-service" {
				t.Errorf("Expected service_name 'test-service', got '%s'", serviceName)
			}
			if podName != "test-pod-1" {
				t.Errorf("Expected pod_name 'test-pod-1', got '%s'", podName)
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "ok",
				"timestamp": time.Now(),
			})
		}
	}))
	defer server.Close()

	// Create client
	client := createTestClient(server.URL)
	client.lastHeartbeatTime = time.Now().Add(-1 * time.Minute) // Set initial time

	// Send heartbeat
	err := client.SendHeartbeat()
	if err != nil {
		t.Fatalf("SendHeartbeat failed: %v", err)
	}

	if !heartbeatReceived {
		t.Error("Heartbeat request was not received by server")
	}

	// Verify last heartbeat time was updated
	if time.Since(client.lastHeartbeatTime) > 1*time.Second {
		t.Error("Last heartbeat time was not updated")
	}
}

func TestClient_SendHeartbeat_ServiceNotFound(t *testing.T) {
	// Create mock server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/heartbeat" {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Service not registered"))
		}
	}))
	defer server.Close()

	client := createTestClient(server.URL)

	err := client.SendHeartbeat()
	if err == nil {
		t.Fatal("Expected error, got nil")
	}

	if !contains(err.Error(), "404") {
		t.Errorf("Expected error to contain '404', got: %v", err)
	}
}

func TestClient_HeartbeatLoop_ReregistersOnTimeout(t *testing.T) {
	registerCount := 0
	heartbeatCount := 0

	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/register":
			registerCount++
			response := models.RegistrationResponse{
				Status:      "success",
				Message:     "Registration completed successfully",
				ServiceName: "test-service",
				Pods: []models.PodInfo{
					createSamplePodInfo("test-pod-1", models.StatusHealthy),
				},
				SubscribedServices: map[string][]models.PodInfo{},
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(response)

		case "/heartbeat":
			heartbeatCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"status":    "ok",
				"timestamp": time.Now(),
			})
		}
	}))
	defer server.Close()

	// Create client with short intervals
	client := NewClient(&ClientConfig{
		ManagerURL:        server.URL,
		ServiceName:       "test-service",
		PodName:           "test-pod-1",
		Timeout:           5 * time.Second,
		HeartbeatInterval: 100 * time.Millisecond,
		HeartbeatTimeout:  250 * time.Millisecond,
	})

	// Initial registration
	registration := &models.ServiceRegistration{
		ServiceName: "test-service",
		PodName:     "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: "test-http", Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
	}
	_, err := client.Register(registration)
	if err != nil {
		t.Fatalf("Initial registration failed: %v", err)
	}

	initialRegisterCount := registerCount

	// Set last heartbeat time to trigger timeout
	client.mu.Lock()
	client.lastHeartbeatTime = time.Now().Add(-1 * time.Second)
	client.mu.Unlock()

	// Start heartbeat
	client.StartHeartbeat()
	defer client.StopHeartbeat()

	// Wait for re-registration to happen
	time.Sleep(400 * time.Millisecond)

	if registerCount <= initialRegisterCount {
		t.Error("Expected re-registration to occur after heartbeat timeout")
	}

	if heartbeatCount == 0 {
		t.Error("Expected at least one heartbeat to be sent")
	}
}

func TestClient_StopHeartbeat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(&ClientConfig{
		ManagerURL:        server.URL,
		ServiceName:       "test-service",
		PodName:           "test-pod-1",
		HeartbeatInterval: 50 * time.Millisecond,
	})

	// Start heartbeat
	client.StartHeartbeat()

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	// Stop heartbeat
	done := make(chan bool)
	go func() {
		client.StopHeartbeat()
		done <- true
	}()

	// Verify it stops within reasonable time
	select {
	case <-done:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("StopHeartbeat did not complete within timeout")
	}
}

func TestCreateSubscribedServicesHandler(t *testing.T) {
	// Create client
	client := createTestClient("http://localhost:8080")

	// Add some pods for subscribed services
	client.UpdatePodInfo("order-service", []models.PodInfo{
		createSamplePodInfo("order-pod-1", models.StatusHealthy),
		createSamplePodInfo("order-pod-2", models.StatusHealthy),
	})

	client.UpdatePodInfo("payment-service", []models.PodInfo{
		createSamplePodInfo("payment-pod-1", models.StatusHealthy),
	})

	// Create test server with subscribed services handler
	handler := client.CreateSubscribedServicesHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	// Test GET request
	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make GET request: %v", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Check content type
	contentType := resp.Header.Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Expected Content-Type 'application/json', got '%s'", contentType)
	}

	// Parse response
	var subsResp SubscribedServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&subsResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify response
	if subsResp.ServiceName != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", subsResp.ServiceName)
	}

	if subsResp.PodName != "test-pod-1" {
		t.Errorf("Expected pod name 'test-pod-1', got '%s'", subsResp.PodName)
	}

	if len(subsResp.SubscribedServices) != 2 {
		t.Fatalf("Expected 2 subscribed services, got %d", len(subsResp.SubscribedServices))
	}

	// Check order-service pods
	orderPods, hasOrder := subsResp.SubscribedServices["order-service"]
	if !hasOrder {
		t.Error("Expected order-service in subscribed services")
	}
	if len(orderPods) != 2 {
		t.Errorf("Expected 2 order-service pods, got %d", len(orderPods))
	}

	// Check payment-service pods
	paymentPods, hasPayment := subsResp.SubscribedServices["payment-service"]
	if !hasPayment {
		t.Error("Expected payment-service in subscribed services")
	}
	if len(paymentPods) != 1 {
		t.Errorf("Expected 1 payment-service pod, got %d", len(paymentPods))
	}

	// Verify timestamp is present
	if subsResp.Timestamp == "" {
		t.Error("Expected timestamp to be present in response")
	}
}

func TestCreateSubscribedServicesHandler_MethodNotAllowed(t *testing.T) {
	client := createTestClient("http://localhost:8080")
	handler := client.CreateSubscribedServicesHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	// Test POST request (should not be allowed)
	resp, err := http.Post(server.URL, "application/json", nil)
	if err != nil {
		t.Fatalf("Failed to make POST request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

func TestCreateSubscribedServicesHandler_EmptyServices(t *testing.T) {
	client := createTestClient("http://localhost:8080")

	// No subscribed services
	handler := client.CreateSubscribedServicesHandler()
	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("Failed to make GET request: %v", err)
	}
	defer resp.Body.Close()

	var subsResp SubscribedServicesResponse
	if err := json.NewDecoder(resp.Body).Decode(&subsResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(subsResp.SubscribedServices) != 0 {
		t.Errorf("Expected 0 subscribed services, got %d", len(subsResp.SubscribedServices))
	}
}

// Helper function to check if string contains substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && indexOf(s, substr) >= 0))
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func TestClient_CheckDiff_NilMaps(t *testing.T) {
	// Test with both nil
	created, updated, deleted := CheckDiff(nil, nil)
	if len(created) != 0 || len(updated) != 0 || len(deleted) != 0 {
		t.Errorf("Expected 0 events with both nil maps, got created=%d, updated=%d, deleted=%d", len(created), len(updated), len(deleted))
	}

	// Test with old nil
	newPods := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
	}
	created, updated, deleted = CheckDiff(nil, newPods)
	if len(created) != 1 {
		t.Fatalf("Expected 1 created event, got %d", len(created))
	}
	if len(updated) != 0 || len(deleted) != 0 {
		t.Errorf("Expected no updated or deleted events, got updated=%d, deleted=%d", len(updated), len(deleted))
	}
	if created[0].Event != "create" {
		t.Errorf("Expected 'create' event, got '%s'", created[0].Event)
	}
	if created[0].Name != "pod-1" {
		t.Errorf("Expected pod name 'pod-1', got '%s'", created[0].Name)
	}

	// Test with new nil
	oldPods := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
	}
	created, updated, deleted = CheckDiff(oldPods, nil)
	if len(deleted) != 1 {
		t.Fatalf("Expected 1 deleted event, got %d", len(deleted))
	}
	if len(created) != 0 || len(updated) != 0 {
		t.Errorf("Expected no created or updated events, got created=%d, updated=%d", len(created), len(updated))
	}
	if deleted[0].Event != "delete" {
		t.Errorf("Expected 'delete' event, got '%s'", deleted[0].Event)
	}
	if deleted[0].Name != "pod-1" {
		t.Errorf("Expected pod name 'pod-1', got '%s'", deleted[0].Name)
	}
}

func TestClient_CheckDiff_CreateEvents(t *testing.T) {
	old := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
	}

	new := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},
		"pod-3": {Name: "pod-3", Ip: "10.0.0.3", Port: 8080},
	}

	created, updated, deleted := CheckDiff(old, new)

	// Should have 2 create events (pod-2 and pod-3)
	if len(created) != 2 {
		t.Fatalf("Expected 2 create events, got %d", len(created))
	}
	if len(updated) != 0 || len(deleted) != 0 {
		t.Errorf("Expected no updated or deleted events, got updated=%d, deleted=%d", len(updated), len(deleted))
	}

	for _, event := range created {
		if event.Name != "pod-2" && event.Name != "pod-3" {
			t.Errorf("Unexpected create event for pod: %s", event.Name)
		}
	}
}

func TestClient_CheckDiff_DeleteEvents(t *testing.T) {
	old := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},
		"pod-3": {Name: "pod-3", Ip: "10.0.0.3", Port: 8080},
	}

	new := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
	}

	created, updated, deleted := CheckDiff(old, new)

	// Should have 2 delete events (pod-2 and pod-3)
	if len(deleted) != 2 {
		t.Fatalf("Expected 2 delete events, got %d", len(deleted))
	}
	if len(created) != 0 || len(updated) != 0 {
		t.Errorf("Expected no created or updated events, got created=%d, updated=%d", len(created), len(updated))
	}

	for _, event := range deleted {
		if event.Name != "pod-2" && event.Name != "pod-3" {
			t.Errorf("Unexpected delete event for pod: %s", event.Name)
		}
	}
}

func TestClient_CheckDiff_UpdateEvents_IPChange(t *testing.T) {
	old := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},
	}

	new := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.10", Port: 8080}, // IP changed
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},  // No change
	}

	created, updated, deleted := CheckDiff(old, new)

	// Should have 1 update event for pod-1
	if len(updated) != 1 {
		t.Fatalf("Expected 1 update event, got %d", len(updated))
	}
	if len(created) != 0 || len(deleted) != 0 {
		t.Errorf("Expected no created or deleted events, got created=%d, deleted=%d", len(created), len(deleted))
	}

	if updated[0].Event != "update" {
		t.Errorf("Expected 'update' event, got '%s'", updated[0].Event)
	}
	if updated[0].Name != "pod-1" {
		t.Errorf("Expected pod name 'pod-1', got '%s'", updated[0].Name)
	}
}

func TestClient_CheckDiff_UpdateEvents_PortChange(t *testing.T) {
	old := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},
	}

	new := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 9090}, // Port changed
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080}, // No change
	}

	created, updated, deleted := CheckDiff(old, new)

	// Should have 1 update event for pod-1
	if len(updated) != 1 {
		t.Fatalf("Expected 1 update event, got %d", len(updated))
	}
	if len(created) != 0 || len(deleted) != 0 {
		t.Errorf("Expected no created or deleted events, got created=%d, deleted=%d", len(created), len(deleted))
	}

	if updated[0].Event != "update" {
		t.Errorf("Expected 'update' event, got '%s'", updated[0].Event)
	}
	if updated[0].Name != "pod-1" {
		t.Errorf("Expected pod name 'pod-1', got '%s'", updated[0].Name)
	}
}

func TestClient_CheckDiff_UpdateEvents_BothChange(t *testing.T) {
	old := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
	}

	new := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.10", Port: 9090}, // Both IP and Port changed
	}

	created, updated, deleted := CheckDiff(old, new)

	// Should have 1 update event for pod-1
	if len(updated) != 1 {
		t.Fatalf("Expected 1 update event, got %d", len(updated))
	}
	if len(created) != 0 || len(deleted) != 0 {
		t.Errorf("Expected no created or deleted events, got created=%d, deleted=%d", len(created), len(deleted))
	}

	if updated[0].Event != "update" {
		t.Errorf("Expected 'update' event, got '%s'", updated[0].Event)
	}
	if updated[0].Name != "pod-1" {
		t.Errorf("Expected pod name 'pod-1', got '%s'", updated[0].Name)
	}
}

func TestClient_CheckDiff_NoChanges(t *testing.T) {
	old := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},
	}

	new := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},
	}

	created, updated, deleted := CheckDiff(old, new)

	// Should have no events
	if len(created) != 0 || len(updated) != 0 || len(deleted) != 0 {
		t.Errorf("Expected 0 events, got created=%d, updated=%d, deleted=%d", len(created), len(updated), len(deleted))
	}
}

func TestClient_CheckDiff_MixedEvents(t *testing.T) {
	old := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.1", Port: 8080},
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},
		"pod-3": {Name: "pod-3", Ip: "10.0.0.3", Port: 8080},
	}

	new := map[string]Pod{
		"pod-1": {Name: "pod-1", Ip: "10.0.0.10", Port: 8080}, // Updated
		"pod-2": {Name: "pod-2", Ip: "10.0.0.2", Port: 8080},  // No change
		"pod-4": {Name: "pod-4", Ip: "10.0.0.4", Port: 8080},  // Created
		// pod-3 deleted
	}

	created, updated, deleted := CheckDiff(old, new)

	// Should have 3 events: 1 update, 1 create, 1 delete
	if len(created) != 1 {
		t.Errorf("Expected 1 create event, got %d", len(created))
	}
	if len(updated) != 1 {
		t.Errorf("Expected 1 update event, got %d", len(updated))
	}
	if len(deleted) != 1 {
		t.Errorf("Expected 1 delete event, got %d", len(deleted))
	}

	// Verify specific events
	if len(created) > 0 && created[0].Name != "pod-4" {
		t.Errorf("Expected create event for 'pod-4', got '%s'", created[0].Name)
	}
	if len(updated) > 0 && updated[0].Name != "pod-1" {
		t.Errorf("Expected update event for 'pod-1', got '%s'", updated[0].Name)
	}
	if len(deleted) > 0 && deleted[0].Name != "pod-3" {
		t.Errorf("Expected delete event for 'pod-3', got '%s'", deleted[0].Name)
	}
}

func TestClient_CheckDiff_EmptyMaps(t *testing.T) {
	old := map[string]Pod{}
	new := map[string]Pod{}

	created, updated, deleted := CheckDiff(old, new)

	if len(created) != 0 || len(updated) != 0 || len(deleted) != 0 {
		t.Errorf("Expected 0 events with empty maps, got created=%d, updated=%d, deleted=%d", len(created), len(updated), len(deleted))
	}
}
