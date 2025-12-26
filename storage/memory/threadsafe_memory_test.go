package memory

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chronnie/governance/models"
)

func createTestService(serviceName, podName string, status models.ServiceStatus) *models.ServiceInfo {
	return &models.ServiceInfo{
		ServiceName: serviceName,
		PodName:     podName,
		Providers: []models.ProviderInfo{
			{
				Protocol: models.ProtocolHTTP,
				IP:       "192.168.1.10",
				Port:     8080,
			},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions: []models.Subscription{{ServiceName: "service-a"}, {ServiceName: "service-b"}},
		Status:          status,
		LastHealthCheck: time.Now(),
		RegisteredAt:    time.Now(),
	}
}

func TestThreadSafeMemoryStore_SaveAndGetService(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	service := createTestService("test-service", "pod-1", models.StatusHealthy)

	// Save service
	err := store.SaveService(ctx, service)
	if err != nil {
		t.Fatalf("Failed to save service: %v", err)
	}

	// Retrieve service
	retrieved, err := store.GetService(ctx, "test-service:pod-1")
	if err != nil {
		t.Fatalf("Failed to get service: %v", err)
	}

	// Verify
	if retrieved.ServiceName != "test-service" {
		t.Errorf("Expected service name 'test-service', got '%s'", retrieved.ServiceName)
	}
	if retrieved.PodName != "pod-1" {
		t.Errorf("Expected pod name 'pod-1', got '%s'", retrieved.PodName)
	}
	if len(retrieved.Providers) != 1 {
		t.Errorf("Expected 1 provider, got %d", len(retrieved.Providers))
	}
}

func TestThreadSafeMemoryStore_SaveService_Nil(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	err := store.SaveService(ctx, nil)
	if err == nil {
		t.Error("Expected error when saving nil service")
	}
	if err.Error() != "service cannot be nil" {
		t.Errorf("Expected 'service cannot be nil' error, got '%s'", err.Error())
	}
}

func TestThreadSafeMemoryStore_GetService_NotFound(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	_, err := store.GetService(ctx, "non-existent:pod-1")
	if err == nil {
		t.Error("Expected error when getting non-existent service")
	}
}

func TestThreadSafeMemoryStore_GetServicesByName(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Save multiple pods for same service
	store.SaveService(ctx, createTestService("test-service", "pod-1", models.StatusHealthy))
	store.SaveService(ctx, createTestService("test-service", "pod-2", models.StatusHealthy))
	store.SaveService(ctx, createTestService("test-service", "pod-3", models.StatusUnhealthy))
	store.SaveService(ctx, createTestService("other-service", "pod-1", models.StatusHealthy))

	// Get services by name
	services, err := store.GetServicesByName(ctx, "test-service")
	if err != nil {
		t.Fatalf("Failed to get services by name: %v", err)
	}

	if len(services) != 3 {
		t.Errorf("Expected 3 services, got %d", len(services))
	}

	// Verify all services have correct name
	for _, svc := range services {
		if svc.ServiceName != "test-service" {
			t.Errorf("Expected service name 'test-service', got '%s'", svc.ServiceName)
		}
	}
}

func TestThreadSafeMemoryStore_GetAllServices(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Save services
	store.SaveService(ctx, createTestService("service-a", "pod-1", models.StatusHealthy))
	store.SaveService(ctx, createTestService("service-b", "pod-1", models.StatusHealthy))
	store.SaveService(ctx, createTestService("service-c", "pod-1", models.StatusHealthy))

	// Get all services
	services, err := store.GetAllServices(ctx)
	if err != nil {
		t.Fatalf("Failed to get all services: %v", err)
	}

	if len(services) != 3 {
		t.Errorf("Expected 3 services, got %d", len(services))
	}
}

func TestThreadSafeMemoryStore_DeleteService(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	service := createTestService("test-service", "pod-1", models.StatusHealthy)
	store.SaveService(ctx, service)

	// Delete service
	err := store.DeleteService(ctx, "test-service:pod-1")
	if err != nil {
		t.Fatalf("Failed to delete service: %v", err)
	}

	// Verify deleted
	_, err = store.GetService(ctx, "test-service:pod-1")
	if err == nil {
		t.Error("Expected error when getting deleted service")
	}
}

func TestThreadSafeMemoryStore_DeleteService_NotFound(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	err := store.DeleteService(ctx, "non-existent:pod-1")
	if err == nil {
		t.Error("Expected error when deleting non-existent service")
	}
}

func TestThreadSafeMemoryStore_UpdateHealthStatus(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	service := createTestService("test-service", "pod-1", models.StatusHealthy)
	store.SaveService(ctx, service)

	// Update health status
	now := time.Now()
	err := store.UpdateHealthStatus(ctx, "test-service:pod-1", models.StatusUnhealthy, now)
	if err != nil {
		t.Fatalf("Failed to update health status: %v", err)
	}

	// Verify update
	updated, _ := store.GetService(ctx, "test-service:pod-1")
	if updated.Status != models.StatusUnhealthy {
		t.Errorf("Expected status 'unhealthy', got '%s'", updated.Status)
	}
	if !updated.LastHealthCheck.Equal(now) {
		t.Errorf("Expected last health check to be updated")
	}
}

func TestThreadSafeMemoryStore_AddSubscription(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Add subscription
	err := store.AddSubscription(ctx, "subscriber:pod-1", "target-service")
	if err != nil {
		t.Fatalf("Failed to add subscription: %v", err)
	}

	// Verify subscription
	subscribers, err := store.GetSubscribers(ctx, "target-service")
	if err != nil {
		t.Fatalf("Failed to get subscribers: %v", err)
	}

	if len(subscribers) != 1 {
		t.Errorf("Expected 1 subscriber, got %d", len(subscribers))
	}
	if subscribers[0] != "subscriber:pod-1" {
		t.Errorf("Expected subscriber 'subscriber:pod-1', got '%s'", subscribers[0])
	}
}

func TestThreadSafeMemoryStore_AddSubscription_Duplicate(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Add same subscription twice
	store.AddSubscription(ctx, "subscriber:pod-1", "target-service")
	store.AddSubscription(ctx, "subscriber:pod-1", "target-service")

	// Verify only one subscription exists
	subscribers, _ := store.GetSubscribers(ctx, "target-service")
	if len(subscribers) != 1 {
		t.Errorf("Expected 1 subscriber, got %d", len(subscribers))
	}
}

func TestThreadSafeMemoryStore_RemoveSubscription(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Add and remove subscription
	store.AddSubscription(ctx, "subscriber:pod-1", "target-service")
	err := store.RemoveSubscription(ctx, "subscriber:pod-1", "target-service")
	if err != nil {
		t.Fatalf("Failed to remove subscription: %v", err)
	}

	// Verify removed
	subscribers, _ := store.GetSubscribers(ctx, "target-service")
	if len(subscribers) != 0 {
		t.Errorf("Expected 0 subscribers, got %d", len(subscribers))
	}
}

func TestThreadSafeMemoryStore_RemoveAllSubscriptions(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Add multiple subscriptions
	store.AddSubscription(ctx, "subscriber:pod-1", "service-a")
	store.AddSubscription(ctx, "subscriber:pod-1", "service-b")
	store.AddSubscription(ctx, "subscriber:pod-1", "service-c")

	// Remove all subscriptions
	err := store.RemoveAllSubscriptions(ctx, "subscriber:pod-1")
	if err != nil {
		t.Fatalf("Failed to remove all subscriptions: %v", err)
	}

	// Verify all removed
	for _, serviceName := range []string{"service-a", "service-b", "service-c"} {
		subscribers, _ := store.GetSubscribers(ctx, serviceName)
		if len(subscribers) != 0 {
			t.Errorf("Expected 0 subscribers for %s, got %d", serviceName, len(subscribers))
		}
	}
}

func TestThreadSafeMemoryStore_GetSubscriberServices(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Create and save subscriber services
	subscriber1 := createTestService("subscriber-service", "pod-1", models.StatusHealthy)
	subscriber2 := createTestService("subscriber-service", "pod-2", models.StatusHealthy)
	store.SaveService(ctx, subscriber1)
	store.SaveService(ctx, subscriber2)

	// Add subscriptions
	store.AddSubscription(ctx, "subscriber-service:pod-1", "target-service")
	store.AddSubscription(ctx, "subscriber-service:pod-2", "target-service")

	// Get subscriber services
	services, err := store.GetSubscriberServices(ctx, "target-service")
	if err != nil {
		t.Fatalf("Failed to get subscriber services: %v", err)
	}

	if len(services) != 2 {
		t.Errorf("Expected 2 subscriber services, got %d", len(services))
	}
}

func TestThreadSafeMemoryStore_Clear(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Add data
	store.SaveService(ctx, createTestService("test-service", "pod-1", models.StatusHealthy))
	store.AddSubscription(ctx, "subscriber:pod-1", "target-service")

	// Clear
	store.Clear()

	// Verify cleared
	services, _ := store.GetAllServices(ctx)
	if len(services) != 0 {
		t.Errorf("Expected 0 services after clear, got %d", len(services))
	}

	serviceCount, subscriptionCount := store.GetStats()
	if serviceCount != 0 || subscriptionCount != 0 {
		t.Errorf("Expected 0 services and subscriptions after clear, got %d/%d", serviceCount, subscriptionCount)
	}
}

func TestThreadSafeMemoryStore_GetStats(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	// Add services and subscriptions
	store.SaveService(ctx, createTestService("service-1", "pod-1", models.StatusHealthy))
	store.SaveService(ctx, createTestService("service-2", "pod-1", models.StatusHealthy))
	store.AddSubscription(ctx, "subscriber:pod-1", "target-service-1")
	store.AddSubscription(ctx, "subscriber:pod-1", "target-service-2")

	serviceCount, subscriptionCount := store.GetStats()
	if serviceCount != 2 {
		t.Errorf("Expected 2 services, got %d", serviceCount)
	}
	if subscriptionCount != 2 {
		t.Errorf("Expected 2 subscription groups, got %d", subscriptionCount)
	}
}

func TestThreadSafeMemoryStore_DeepCopy(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	service := createTestService("test-service", "pod-1", models.StatusHealthy)
	store.SaveService(ctx, service)

	// Get service
	retrieved, _ := store.GetService(ctx, "test-service:pod-1")

	// Modify retrieved service
	retrieved.ServiceName = "modified"
	retrieved.Providers[0].Port = 9999
	retrieved.Subscriptions[0] = models.Subscription{ServiceName: "modified-subscription"}

	// Get service again
	retrieved2, _ := store.GetService(ctx, "test-service:pod-1")

	// Verify original is unchanged
	if retrieved2.ServiceName != "test-service" {
		t.Error("Deep copy failed: service name was modified")
	}
	if retrieved2.Providers[0].Port != 8080 {
		t.Error("Deep copy failed: provider was modified")
	}
	if retrieved2.Subscriptions[0].ServiceName != "service-a" {
		t.Error("Deep copy failed: subscription was modified")
	}
}

func TestThreadSafeMemoryStore_ConcurrentAccess(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	var wg sync.WaitGroup
	iterations := 100

	// Concurrent writes
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				service := createTestService("test-service", "pod-"+string(rune(id)), models.StatusHealthy)
				store.SaveService(ctx, service)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				store.GetAllServices(ctx)
				store.GetServicesByName(ctx, "test-service")
			}
		}()
	}

	// Concurrent subscription operations
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				subscriberKey := "subscriber:pod-" + string(rune(id))
				store.AddSubscription(ctx, subscriberKey, "target-service")
				store.GetSubscribers(ctx, "target-service")
			}
		}(i)
	}

	wg.Wait()

	// Verify store is still consistent
	services, _ := store.GetAllServices(ctx)
	if len(services) == 0 {
		t.Error("Expected services after concurrent access")
	}
}

func TestThreadSafeMemoryStore_Ping(t *testing.T) {
	store := NewThreadSafeMemoryStore()
	ctx := context.Background()

	err := store.Ping(ctx)
	if err != nil {
		t.Errorf("Ping should always succeed for memory store, got error: %v", err)
	}
}

func TestThreadSafeMemoryStore_Close(t *testing.T) {
	store := NewThreadSafeMemoryStore()

	err := store.Close()
	if err != nil {
		t.Errorf("Close should always succeed for memory store, got error: %v", err)
	}
}
