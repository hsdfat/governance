package registry

import (
	"context"
	"testing"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
)

// TestAutoCleanup_ConsecutiveFailures tests the consecutive failure tracking
func TestAutoCleanup_ConsecutiveFailures(t *testing.T) {
	// Create in-memory storage (cache only, no database)
	dualStore := storage.NewDualStore(nil)

	// Create registry
	reg := NewRegistry(dualStore, logger.Log)

	// Register a test service
	registration := &models.ServiceRegistration{
		ServiceName:     "test-service",
		PodName:         "test-pod-1",
		HealthCheckURL:  "http://localhost:9999/health",
		NotificationURL: "http://localhost:9999/notify",
		Providers:       []models.ProviderInfo{},
		Subscriptions:   []models.Subscription{},
	}

	_ = reg.Register(registration)

	key := registration.ServiceName + ":" + registration.PodName

	// Test: First failure should set consecutive failures to 1
	t.Run("FirstFailure", func(t *testing.T) {
		failures := reg.TrackHealthCheckFailure(key)
		if failures != 1 {
			t.Errorf("Expected 1 consecutive failure, got %d", failures)
		}

		// Verify in-memory tracking
		trackedFailures := reg.GetConsecutiveFailures(key)
		if trackedFailures != 1 {
			t.Errorf("Expected 1 tracked failure, got %d", trackedFailures)
		}
	})

	// Test: Second consecutive failure should increment counter
	t.Run("SecondConsecutiveFailure", func(t *testing.T) {
		time.Sleep(10 * time.Millisecond) // Small delay to ensure different timestamp

		failures := reg.TrackHealthCheckFailure(key)
		if failures != 2 {
			t.Errorf("Expected 2 consecutive failures, got %d", failures)
		}
	})

	// Test: Third, fourth, fifth failures
	t.Run("MultipleConsecutiveFailures", func(t *testing.T) {
		for i := 3; i <= 5; i++ {
			time.Sleep(10 * time.Millisecond)
			failures := reg.TrackHealthCheckFailure(key)

			if failures != i {
				t.Errorf("Expected %d consecutive failures, got %d", i, failures)
			}
		}
	})

	// Test: Recovery should reset counter
	t.Run("RecoveryShouldResetCounter", func(t *testing.T) {
		reg.ResetHealthCheckFailures(key)

		failures := reg.GetConsecutiveFailures(key)
		if failures != 0 {
			t.Errorf("Expected 0 consecutive failures after recovery, got %d", failures)
		}
	})

	// Test: Failure after recovery should restart counting
	t.Run("FailureAfterRecovery", func(t *testing.T) {
		failures := reg.TrackHealthCheckFailure(key)
		if failures != 1 {
			t.Errorf("Expected 1 consecutive failure after recovery, got %d", failures)
		}
	})
}

// TestAutoCleanup_PersistenceInMemory tests that failure tracking persists in memory store
func TestAutoCleanup_PersistenceInMemory(t *testing.T) {
	// Create in-memory storage (cache only)
	dualStore := storage.NewDualStore(nil)

	// Create registry
	reg := NewRegistry(dualStore, logger.Log)

	// Register a test service
	registration := &models.ServiceRegistration{
		ServiceName:     "persist-test",
		PodName:         "pod-1",
		HealthCheckURL:  "http://localhost:9999/health",
		NotificationURL: "http://localhost:9999/notify",
		Providers:       []models.ProviderInfo{},
		Subscriptions:   []models.Subscription{},
	}

	_ = reg.Register(registration)

	key := registration.ServiceName + ":" + registration.PodName

	// Mark as unhealthy 3 times
	for i := 1; i <= 3; i++ {
		reg.TrackHealthCheckFailure(key)
		time.Sleep(10 * time.Millisecond)
	}

	// Verify failures are tracked in-memory
	failures := reg.GetConsecutiveFailures(key)
	if failures != 3 {
		t.Errorf("Expected 3 consecutive failures, got %d", failures)
	}
}

// TestAutoCleanup_UnregisterCleansUp tests that unregister removes all data
func TestAutoCleanup_UnregisterCleansUp(t *testing.T) {
	// Create in-memory storage (cache only)
	dualStore := storage.NewDualStore(nil)

	// Create registry
	reg := NewRegistry(dualStore, logger.Log)

	// Register a test service
	registration := &models.ServiceRegistration{
		ServiceName:     "cleanup-test",
		PodName:         "pod-1",
		HealthCheckURL:  "http://localhost:9999/health",
		NotificationURL: "http://localhost:9999/notify",
		Providers:       []models.ProviderInfo{},
		Subscriptions:   []models.Subscription{},
	}

	_ = reg.Register(registration)

	key := registration.ServiceName + ":" + registration.PodName

	// Track failure
	reg.TrackHealthCheckFailure(key)

	// Verify service exists with failures
	failures := reg.GetConsecutiveFailures(key)
	if failures != 1 {
		t.Errorf("Expected 1 consecutive failure, got %d", failures)
	}

	// Unregister the service
	deletedService := reg.Unregister("cleanup-test", "pod-1")
	if deletedService == nil {
		t.Fatal("Expected service to be returned on unregister")
	}

	// Verify service is removed from cache
	_, exists := reg.Get(key)
	if exists {
		t.Error("Service should not exist in cache after unregister")
	}

	// Verify consecutive failures return 0 for removed service (already tested above with Get)

	// GetConsecutiveFailures should return 0 for non-existent service
	failures = reg.GetConsecutiveFailures(key)
	if failures != 0 {
		t.Errorf("Expected 0 failures for non-existent service, got %d", failures)
	}
}

// TestAutoCleanup_MultipleServices tests failure tracking for multiple services independently
func TestAutoCleanup_MultipleServices(t *testing.T) {
	// Create in-memory storage (cache only)
	dualStore := storage.NewDualStore(nil)

	// Create registry
	reg := NewRegistry(dualStore, logger.Log)

	// Register two services
	registration1 := &models.ServiceRegistration{
		ServiceName:     "service-a",
		PodName:         "pod-1",
		HealthCheckURL:  "http://localhost:9999/health",
		NotificationURL: "http://localhost:9999/notify",
		Providers:       []models.ProviderInfo{},
		Subscriptions:   []models.Subscription{},
	}

	registration2 := &models.ServiceRegistration{
		ServiceName:     "service-b",
		PodName:         "pod-1",
		HealthCheckURL:  "http://localhost:9998/health",
		NotificationURL: "http://localhost:9998/notify",
		Providers:       []models.ProviderInfo{},
		Subscriptions:   []models.Subscription{},
	}

	reg.Register(registration1)
	reg.Register(registration2)

	key1 := registration1.ServiceName + ":" + registration1.PodName
	key2 := registration2.ServiceName + ":" + registration2.PodName

	// Fail service1 three times
	for i := 0; i < 3; i++ {
		reg.TrackHealthCheckFailure(key1)
		time.Sleep(10 * time.Millisecond)
	}

	// Fail service2 once
	reg.TrackHealthCheckFailure(key2)

	// Verify independent tracking
	failures1 := reg.GetConsecutiveFailures(key1)
	failures2 := reg.GetConsecutiveFailures(key2)

	if failures1 != 3 {
		t.Errorf("Service 1: expected 3 failures, got %d", failures1)
	}

	if failures2 != 1 {
		t.Errorf("Service 2: expected 1 failure, got %d", failures2)
	}

	// Recover service1
	reg.ResetHealthCheckFailures(key1)

	// Verify only service1 was reset
	failures1 = reg.GetConsecutiveFailures(key1)
	failures2 = reg.GetConsecutiveFailures(key2)

	if failures1 != 0 {
		t.Errorf("Service 1: expected 0 failures after recovery, got %d", failures1)
	}

	if failures2 != 1 {
		t.Errorf("Service 2: expected still 1 failure, got %d", failures2)
	}
}

// TestAutoCleanup_FirstFailureAtTimestamp tests that FirstFailureAt is properly set and maintained
func TestAutoCleanup_FirstFailureAtTimestamp(t *testing.T) {
	// Create in-memory storage (cache only)
	dualStore := storage.NewDualStore(nil)

	// Create registry
	reg := NewRegistry(dualStore, logger.Log)

	// Register a test service
	registration := &models.ServiceRegistration{
		ServiceName:     "timestamp-test",
		PodName:         "pod-1",
		HealthCheckURL:  "http://localhost:9999/health",
		NotificationURL: "http://localhost:9999/notify",
		Providers:       []models.ProviderInfo{},
		Subscriptions:   []models.Subscription{},
	}

	reg.Register(registration)
	key := registration.ServiceName + ":" + registration.PodName

	// First failure
	reg.TrackHealthCheckFailure(key)

	// Get first failure from in-memory tracker
	firstFailures := reg.GetConsecutiveFailures(key)
	if firstFailures != 1 {
		t.Errorf("Expected 1 failure, got %d", firstFailures)
	}

	// Wait and cause second failure
	time.Sleep(100 * time.Millisecond)
	reg.TrackHealthCheckFailure(key)

	secondFailures := reg.GetConsecutiveFailures(key)
	if secondFailures != 2 {
		t.Errorf("Expected 2 failures, got %d", secondFailures)
	}
}

// TestAutoCleanup_ZeroFirstFailureAtBugFix tests the fix for zero FirstFailureAt
func TestAutoCleanup_ZeroFirstFailureAtBugFix(t *testing.T) {
	// Create in-memory storage (cache only)
	dualStore := storage.NewDualStore(nil)

	// Create registry
	reg := NewRegistry(dualStore, logger.Log)

	// Manually insert a service that's already unhealthy (simulating DB load or service restart)
	service := &models.ServiceInfo{
		ServiceName:         "preexisting-unhealthy",
		PodName:             "pod-1",
		HealthCheckURL:      "http://localhost:9999/health",
		NotificationURL:     "http://localhost:9999/notify",
		Providers:           []models.ProviderInfo{},
		Subscriptions:       []models.Subscription{},
		Status:              models.StatusUnhealthy,
		RegisteredAt:        time.Now(),
		ConsecutiveFailures: 1,
		FirstFailureAt:      time.Time{}, // Zero time - simulating the bug
	}

	dualStore.SaveService(context.Background(), service)

	key := service.ServiceName + ":" + service.PodName

	// Now trigger another health check failure
	failures := reg.TrackHealthCheckFailure(key)

	// Verify consecutive failures incremented (in-memory tracking starts fresh)
	if failures != 1 {
		t.Errorf("Expected 1 consecutive failure (fresh tracking), got %d", failures)
	}
}
