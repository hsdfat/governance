package notifier

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chronnie/governance/models"
)

func TestNewNotifier(t *testing.T) {
	timeout := 5 * time.Second
	notif := NewNotifier(timeout)

	if notif == nil {
		t.Fatal("NewNotifier returned nil")
	}
	if notif.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, notif.timeout)
	}
	if notif.httpClient == nil {
		t.Error("httpClient not initialized")
	}
}

func TestNewHealthChecker(t *testing.T) {
	timeout := 5 * time.Second
	maxRetries := 3
	hc := NewHealthChecker(timeout, maxRetries)

	if hc == nil {
		t.Fatal("NewHealthChecker returned nil")
	}
	if hc.timeout != timeout {
		t.Errorf("Expected timeout %v, got %v", timeout, hc.timeout)
	}
	if hc.maxRetries != maxRetries {
		t.Errorf("Expected maxRetries %d, got %d", maxRetries, hc.maxRetries)
	}
}

func TestCheckHealthSuccess(t *testing.T) {
	// Create test server that returns 200
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hc := NewHealthChecker(5*time.Second, 3)
	healthy := hc.CheckHealth(server.URL)

	if !healthy {
		t.Error("Expected health check to pass")
	}
}

func TestCheckHealthFailure(t *testing.T) {
	// Create test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	hc := NewHealthChecker(1*time.Second, 1) // Low retry for faster test
	healthy := hc.CheckHealth(server.URL)

	if healthy {
		t.Error("Expected health check to fail")
	}
}

func TestCheckHealthInvalidURL(t *testing.T) {
	hc := NewHealthChecker(1*time.Second, 1)
	healthy := hc.CheckHealth("http://invalid-url-that-does-not-exist:99999/health")

	if healthy {
		t.Error("Expected health check to fail for invalid URL")
	}
}

func TestGetHealthStatus(t *testing.T) {
	// Test healthy status
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hc := NewHealthChecker(5*time.Second, 3)
	status := hc.GetHealthStatus(server.URL)

	if status != models.StatusHealthy {
		t.Errorf("Expected status 'healthy', got '%s'", status)
	}

	// Test unhealthy status
	status = hc.GetHealthStatus("http://invalid-url:99999/health")
	if status != models.StatusUnhealthy {
		t.Errorf("Expected status 'unhealthy', got '%s'", status)
	}
}

func TestBuildNotificationPayload(t *testing.T) {
	pods := []*models.ServiceInfo{
		{
			ServiceName: "test-service",
			PodName:     "pod-1",
			Status:      models.StatusHealthy,
			Providers: []models.ProviderInfo{
				{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
			},
		},
		{
			ServiceName: "test-service",
			PodName:     "pod-2",
			Status:      models.StatusHealthy,
			Providers: []models.ProviderInfo{
				{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.11", Port: 8080},
			},
		},
	}

	payload := BuildNotificationPayload("test-service", models.EventTypeRegister, pods)

	if payload.ServiceName != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", payload.ServiceName)
	}
	if payload.EventType != models.EventTypeRegister {
		t.Errorf("Expected event type 'register', got '%s'", payload.EventType)
	}
	if len(payload.Pods) != 2 {
		t.Errorf("Expected 2 pods, got %d", len(payload.Pods))
	}
	if payload.Pods[0].PodName != "pod-1" {
		t.Errorf("Expected pod name 'pod-1', got '%s'", payload.Pods[0].PodName)
	}
	if payload.Pods[0].Status != models.StatusHealthy {
		t.Errorf("Expected status 'healthy', got '%s'", payload.Pods[0].Status)
	}
}

func TestNotifySubscriberSuccess(t *testing.T) {
	// Track if notification was received
	received := false
	var receivedPayload models.NotificationPayload

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = true

		// Decode payload
		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			t.Errorf("Failed to decode payload: %v", err)
		}

		// Verify content type
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Expected Content-Type: application/json")
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notif := NewNotifier(5 * time.Second)
	payload := &models.NotificationPayload{
		ServiceName: "test-service",
		EventType:   models.EventTypeRegister,
		Timestamp:   time.Now(),
		Pods:        []models.PodInfo{},
	}

	notif.NotifySubscriber(server.URL, payload)

	// Wait for async notification to complete
	time.Sleep(100 * time.Millisecond)

	if !received {
		t.Error("Notification was not received")
	}
	if receivedPayload.ServiceName != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", receivedPayload.ServiceName)
	}
}

func TestNotifySubscribersFailed(t *testing.T) {
	// Create test server that returns error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	notif := NewNotifier(1 * time.Second)
	payload := &models.NotificationPayload{
		ServiceName: "test-service",
		EventType:   models.EventTypeRegister,
		Timestamp:   time.Now(),
		Pods:        []models.PodInfo{},
	}

	// Should not panic on failure
	notif.NotifySubscriber(server.URL, payload)
	time.Sleep(100 * time.Millisecond)
}

func TestNotifyMultipleSubscribers(t *testing.T) {
	count := 0

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	notif := NewNotifier(5 * time.Second)
	payload := &models.NotificationPayload{
		ServiceName: "test-service",
		EventType:   models.EventTypeRegister,
		Timestamp:   time.Now(),
		Pods:        []models.PodInfo{},
	}

	subscribers := []*models.ServiceInfo{
		{NotificationURL: server.URL},
		{NotificationURL: server.URL},
		{NotificationURL: server.URL},
	}

	notif.NotifySubscribers(subscribers, payload)

	// Wait for async notifications to complete
	time.Sleep(200 * time.Millisecond)

	if count != 3 {
		t.Errorf("Expected 3 notifications, got %d", count)
	}
}

func TestHealthCheckWith2xxStatusCodes(t *testing.T) {
	testCases := []int{200, 201, 204, 299}

	for _, statusCode := range testCases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(statusCode)
		}))

		hc := NewHealthChecker(5*time.Second, 1)
		healthy := hc.CheckHealth(server.URL)

		server.Close()

		if !healthy {
			t.Errorf("Expected health check to pass for status code %d", statusCode)
		}
	}
}

func TestHealthCheckWithNon2xxStatusCodes(t *testing.T) {
	testCases := []int{404, 500, 503}

	for _, statusCode := range testCases {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(statusCode)
		}))

		hc := NewHealthChecker(1*time.Second, 1)
		healthy := hc.CheckHealth(server.URL)

		server.Close()

		if healthy {
			t.Errorf("Expected health check to fail for status code %d", statusCode)
		}
	}
}

func TestHealthCheckRetry(t *testing.T) {
	attempts := 0
	maxRetries := 2

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		// Always fail
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	hc := NewHealthChecker(500*time.Millisecond, maxRetries)
	healthy := hc.CheckHealth(server.URL)

	if healthy {
		t.Error("Expected health check to fail")
	}

	// Should attempt initial try + maxRetries
	expectedAttempts := maxRetries + 1
	if attempts != expectedAttempts {
		t.Errorf("Expected %d attempts, got %d", expectedAttempts, attempts)
	}
}
