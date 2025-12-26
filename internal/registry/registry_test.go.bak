package registry

import (
	"testing"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage"
)

func TestNewRegistry(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)
	if reg == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if reg.store == nil {
		t.Error("store not initialized")
	}
}

func TestRegister(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	registration := &models.ServiceRegistration{
		ServiceName: "test-service",
		PodName:     "test-pod-1",
		Providers: []models.ProviderInfo{
			{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []string{"other-service"},
	}

	serviceInfo := reg.Register(registration)

	// Verify service info
	if serviceInfo.ServiceName != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", serviceInfo.ServiceName)
	}
	if serviceInfo.PodName != "test-pod-1" {
		t.Errorf("Expected pod name 'test-pod-1', got '%s'", serviceInfo.PodName)
	}
	if serviceInfo.Status != models.StatusUnknown {
		t.Errorf("Expected initial status 'unknown', got '%s'", serviceInfo.Status)
	}

	// Verify service is in registry
	key := "test-service:test-pod-1"
	retrieved, exists := reg.Get(key)
	if !exists {
		t.Error("Service not found in registry")
	}
	if retrieved.ServiceName != "test-service" {
		t.Error("Retrieved service has wrong name")
	}

	// Verify subscriptions are registered
	subscribers := reg.GetSubscribers("other-service")
	if len(subscribers) != 1 {
		t.Errorf("Expected 1 subscriber, got %d", len(subscribers))
	}
	if subscribers[0] != key {
		t.Errorf("Expected subscriber key '%s', got '%s'", key, subscribers[0])
	}
}

func TestRegisterUpdate(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	// First registration
	reg1 := &models.ServiceRegistration{
		ServiceName:     "test-service",
		PodName:         "test-pod-1",
		Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []string{"service-a"},
	}
	reg.Register(reg1)

	// Update registration with different subscriptions
	reg2 := &models.ServiceRegistration{
		ServiceName:     "test-service",
		PodName:         "test-pod-1",
		Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 9090}},
		HealthCheckURL:  "http://192.168.1.10:9090/health",
		NotificationURL: "http://192.168.1.10:9090/notify",
		Subscriptions:   []string{"service-b"},
	}
	serviceInfo := reg.Register(reg2)

	// Verify update
	if serviceInfo.Providers[0].Port != 9090 {
		t.Errorf("Expected port 9090, got %d", serviceInfo.Providers[0].Port)
	}

	// Verify old subscription removed
	subscribersA := reg.GetSubscribers("service-a")
	if len(subscribersA) != 0 {
		t.Error("Old subscription should be removed")
	}

	// Verify new subscription added
	subscribersB := reg.GetSubscribers("service-b")
	if len(subscribersB) != 1 {
		t.Errorf("Expected 1 subscriber for service-b, got %d", len(subscribersB))
	}
}

func TestUnregister(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	// Register service
	registration := &models.ServiceRegistration{
		ServiceName:     "test-service",
		PodName:         "test-pod-1",
		Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []string{"other-service"},
	}
	reg.Register(registration)

	// Unregister
	serviceInfo := reg.Unregister("test-service", "test-pod-1")
	if serviceInfo == nil {
		t.Fatal("Unregister returned nil")
	}
	if serviceInfo.ServiceName != "test-service" {
		t.Error("Unregistered service has wrong name")
	}

	// Verify service removed from registry
	key := "test-service:test-pod-1"
	_, exists := reg.Get(key)
	if exists {
		t.Error("Service should be removed from registry")
	}

	// Verify subscriptions removed
	subscribers := reg.GetSubscribers("other-service")
	if len(subscribers) != 0 {
		t.Error("Subscriptions should be removed")
	}
}

func TestUnregisterNonExistent(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	serviceInfo := reg.Unregister("non-existent", "pod-1")
	if serviceInfo != nil {
		t.Error("Unregister should return nil for non-existent service")
	}
}

func TestGetByServiceName(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	// Register multiple pods of same service
	for i := 1; i <= 3; i++ {
		registration := &models.ServiceRegistration{
			ServiceName:     "test-service",
			PodName:         "test-pod-" + string(rune('0'+i)),
			Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080 + i}},
			HealthCheckURL:  "http://192.168.1.10:8080/health",
			NotificationURL: "http://192.168.1.10:8080/notify",
			Subscriptions:   []string{},
		}
		reg.Register(registration)
	}

	// Get all pods
	pods := reg.GetByServiceName("test-service")
	if len(pods) != 3 {
		t.Errorf("Expected 3 pods, got %d", len(pods))
	}
}

func TestGetAllServices(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	// Register multiple services
	for i := 1; i <= 5; i++ {
		registration := &models.ServiceRegistration{
			ServiceName:     "service-" + string(rune('0'+i)),
			PodName:         "pod-1",
			Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
			HealthCheckURL:  "http://192.168.1.10:8080/health",
			NotificationURL: "http://192.168.1.10:8080/notify",
			Subscriptions:   []string{},
		}
		reg.Register(registration)
	}

	services := reg.GetAllServices()
	if len(services) != 5 {
		t.Errorf("Expected 5 services, got %d", len(services))
	}
}

func TestUpdateHealthStatus(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	// Register service
	registration := &models.ServiceRegistration{
		ServiceName:     "test-service",
		PodName:         "test-pod-1",
		Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []string{},
	}
	reg.Register(registration)

	key := "test-service:test-pod-1"

	// Update to healthy
	changed := reg.UpdateHealthStatus(key, models.StatusHealthy)
	if !changed {
		t.Error("Status should have changed from unknown to healthy")
	}

	service, _ := reg.Get(key)
	if service.Status != models.StatusHealthy {
		t.Errorf("Expected status 'healthy', got '%s'", service.Status)
	}

	// Update to same status
	changed = reg.UpdateHealthStatus(key, models.StatusHealthy)
	if changed {
		t.Error("Status should not have changed when updating to same status")
	}

	// Update to unhealthy
	changed = reg.UpdateHealthStatus(key, models.StatusUnhealthy)
	if !changed {
		t.Error("Status should have changed from healthy to unhealthy")
	}
}

func TestUpdateHealthStatusNonExistent(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	changed := reg.UpdateHealthStatus("non-existent:pod-1", models.StatusHealthy)
	if changed {
		t.Error("Should return false for non-existent service")
	}
}

func TestGetSubscriberServices(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	// Register subscribers
	for i := 1; i <= 3; i++ {
		registration := &models.ServiceRegistration{
			ServiceName:     "subscriber-" + string(rune('0'+i)),
			PodName:         "pod-1",
			Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
			HealthCheckURL:  "http://192.168.1.10:8080/health",
			NotificationURL: "http://192.168.1.10:8080/notify",
			Subscriptions:   []string{"target-service"},
		}
		reg.Register(registration)
	}

	subscribers := reg.GetSubscriberServices("target-service")
	if len(subscribers) != 3 {
		t.Errorf("Expected 3 subscriber services, got %d", len(subscribers))
	}
}

func TestMultipleSubscriptions(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	// Register service with multiple subscriptions
	registration := &models.ServiceRegistration{
		ServiceName:     "subscriber-service",
		PodName:         "pod-1",
		Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []string{"service-a", "service-b", "service-c"},
	}
	reg.Register(registration)

	key := "subscriber-service:pod-1"

	// Verify subscriptions
	for _, serviceName := range []string{"service-a", "service-b", "service-c"} {
		subscribers := reg.GetSubscribers(serviceName)
		if len(subscribers) != 1 {
			t.Errorf("Expected 1 subscriber for %s, got %d", serviceName, len(subscribers))
		}
		if subscribers[0] != key {
			t.Errorf("Expected subscriber key '%s', got '%s'", key, subscribers[0])
		}
	}
}

func TestServiceInfoGetKey(t *testing.T) {
	service := &models.ServiceInfo{
		ServiceName: "test-service",
		PodName:     "test-pod-1",
	}

	expected := "test-service:test-pod-1"
	if service.GetKey() != expected {
		t.Errorf("Expected key '%s', got '%s'", expected, service.GetKey())
	}
}

func TestRegisteredAtTimestamp(t *testing.T) {
	dualStore := storage.NewDualStore(nil)
	reg := NewRegistry(dualStore)

	before := time.Now()
	registration := &models.ServiceRegistration{
		ServiceName:     "test-service",
		PodName:         "test-pod-1",
		Providers:       []models.ProviderInfo{{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080}},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions:   []string{},
	}
	serviceInfo := reg.Register(registration)
	after := time.Now()

	if serviceInfo.RegisteredAt.Before(before) || serviceInfo.RegisteredAt.After(after) {
		t.Error("RegisteredAt timestamp is not within expected range")
	}
}
