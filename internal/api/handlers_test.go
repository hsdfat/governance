package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
	eventqueue "github.com/hsdfat/telco/equeue"
)

func setupTestHandler() (*Handler, *registry.Registry, eventqueue.IEventQueue) {
	log := logger.Log
	dualStore := storage.NewDualStore(nil)
	reg := registry.NewRegistry(dualStore, log)
	queueConfig := eventqueue.EventQueueConfig{
		BufferSize:     100,
		ProcessingMode: eventqueue.Sequential,
	}
	queue := eventqueue.NewEventQueue(queueConfig)

	// Start queue in background
	go queue.Start(context.Background())

	handler := NewHandler(reg, queue, log)
	return handler, reg, queue
}

func TestRegisterHandlerSuccess(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	registration := &models.ServiceRegistration{
		ServiceName: "test-service",
		PodName:     "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []models.Subscription{{ServiceName: "other-service"}},
	}

	jsonData, _ := json.Marshal(registration)
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(jsonData))
	rec := httptest.NewRecorder()

	handler.RegisterHandler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, rec.Code)
	}

	var response map[string]string
	json.NewDecoder(rec.Body).Decode(&response)

	if response["status"] != "accepted" {
		t.Errorf("Expected status 'accepted', got '%s'", response["status"])
	}
}

func TestRegisterHandlerInvalidJSON(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBufferString("invalid json"))
	rec := httptest.NewRecorder()

	handler.RegisterHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestRegisterHandlerMissingServiceName(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	registration := &models.ServiceRegistration{
		PodName: "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
	}

	jsonData, _ := json.Marshal(registration)
	req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewBuffer(jsonData))
	rec := httptest.NewRecorder()

	handler.RegisterHandler(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}
}

func TestRegisterHandlerMethodNotAllowed(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	rec := httptest.NewRecorder()

	handler.RegisterHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestUnregisterHandlerSuccess(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	// Give queue time to start
	time.Sleep(50 * time.Millisecond)

	req := httptest.NewRequest(http.MethodDelete, "/unregister?service_name=test-service&pod_name=test-pod-1", nil)
	rec := httptest.NewRecorder()

	handler.UnregisterHandler(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, rec.Code)
	}

	var response map[string]string
	json.NewDecoder(rec.Body).Decode(&response)

	if response["status"] != "accepted" {
		t.Errorf("Expected status 'accepted', got '%s'", response["status"])
	}
}

func TestUnregisterHandlerMissingParameters(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	testCases := []string{
		"/unregister",
		"/unregister?service_name=test-service",
		"/unregister?pod_name=test-pod-1",
	}

	for _, url := range testCases {
		req := httptest.NewRequest(http.MethodDelete, url, nil)
		rec := httptest.NewRecorder()

		handler.UnregisterHandler(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected status %d for URL %s, got %d", http.StatusBadRequest, url, rec.Code)
		}
	}
}

func TestUnregisterHandlerMethodNotAllowed(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	req := httptest.NewRequest(http.MethodPost, "/unregister?service_name=test&pod_name=pod1", nil)
	rec := httptest.NewRecorder()

	handler.UnregisterHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestServicesHandler(t *testing.T) {
	handler, reg, queue := setupTestHandler()
	defer queue.Stop()

	// Register some services
	for i := 1; i <= 3; i++ {
		registration := &models.ServiceRegistration{
			ServiceName:     "test-service",
			PodName:         "pod-" + string(rune('0'+i)),
			Providers:       []models.ProviderInfo{{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
			HealthCheckURL:  "http://192.168.1.10:8080/health",
			NotificationURL: "http://192.168.1.10:8080/notify",
			Subscriptions:   []models.Subscription{},
		}
		reg.Register(registration)
	}

	req := httptest.NewRequest(http.MethodGet, "/services", nil)
	rec := httptest.NewRecorder()

	handler.ServicesHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var response map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&response)

	count := int(response["count"].(float64))
	if count != 3 {
		t.Errorf("Expected count 3, got %d", count)
	}
}

func TestServicesHandlerMethodNotAllowed(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	req := httptest.NewRequest(http.MethodPost, "/services", nil)
	rec := httptest.NewRecorder()

	handler.ServicesHandler(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status %d, got %d", http.StatusMethodNotAllowed, rec.Code)
	}
}

func TestHealthHandler(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	handler.HealthHandler(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, rec.Code)
	}

	var response map[string]string
	json.NewDecoder(rec.Body).Decode(&response)

	if response["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response["status"])
	}
}

func TestValidateRegistration(t *testing.T) {
	handler, _, queue := setupTestHandler()
	defer queue.Stop()

	// Valid registration
	validReg := &models.ServiceRegistration{
		ServiceName: "test-service",
		PodName:     "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []models.Subscription{},
	}

	err := handler.validateRegistration(validReg)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Test missing service name
	invalidReg := &models.ServiceRegistration{
		PodName: "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
	}
	err = handler.validateRegistration(invalidReg)
	if err == nil {
		t.Error("Expected error for missing service name")
	}

	// Test missing pod name
	invalidReg = &models.ServiceRegistration{
		ServiceName: "test-service",
		Providers: []models.ProviderInfo{
			{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
	}
	err = handler.validateRegistration(invalidReg)
	if err == nil {
		t.Error("Expected error for missing pod name")
	}

	// Test empty providers
	invalidReg = &models.ServiceRegistration{
		ServiceName:     "test-service",
		PodName:         "test-pod-1",
		Providers:       []models.ProviderInfo{},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
	}
	err = handler.validateRegistration(invalidReg)
	if err == nil {
		t.Error("Expected error for empty providers")
	}

	// Test invalid port
	invalidReg = &models.ServiceRegistration{
		ServiceName: "test-service",
		PodName:     "test-pod-1",
		Providers: []models.ProviderInfo{
			{ProviderID: string(models.ProviderHTTP), Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 99999},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
	}
	err = handler.validateRegistration(invalidReg)
	if err == nil {
		t.Error("Expected error for invalid port")
	}
}

func TestValidationError(t *testing.T) {
	err := &ValidationError{Message: "test error"}
	if err.Error() != "test error" {
		t.Errorf("Expected 'test error', got '%s'", err.Error())
	}

	index := 2
	err = &ValidationError{Message: "test error", Index: &index}
	errMsg := err.Error()
	if errMsg == "test error" {
		t.Error("Expected error message to include index")
	}
}
