package memory

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage"
)

// ThreadSafeMemoryStore implements storage.RegistryStore using in-memory maps with thread-safety.
// This version uses RWMutex for concurrent access protection, making it suitable for
// multi-threaded scenarios, testing, and applications that need concurrent access.
type ThreadSafeMemoryStore struct {
	mu            sync.RWMutex
	services      map[string]*models.ServiceInfo // Key: "serviceName:podName"
	subscriptions map[string][]string            // Key: serviceGroup, Value: list of subscriber keys
}

// Ensure ThreadSafeMemoryStore implements RegistryStore
var _ storage.RegistryStore = (*ThreadSafeMemoryStore)(nil)

// NewThreadSafeMemoryStore creates a new thread-safe in-memory storage instance
func NewThreadSafeMemoryStore() *ThreadSafeMemoryStore {
	return &ThreadSafeMemoryStore{
		services:      make(map[string]*models.ServiceInfo),
		subscriptions: make(map[string][]string),
	}
}

// SaveService stores or updates a service entry
func (m *ThreadSafeMemoryStore) SaveService(ctx context.Context, service *models.ServiceInfo) error {
	if service == nil {
		return errors.New("service cannot be nil")
	}

	key := service.GetKey()
	if key == "" {
		return errors.New("service key cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Store a copy to avoid external mutations
	serviceCopy := *service

	// Deep copy slices
	if service.Providers != nil {
		serviceCopy.Providers = make([]models.ProviderInfo, len(service.Providers))
		copy(serviceCopy.Providers, service.Providers)
	}
	if service.Subscriptions != nil {
		serviceCopy.Subscriptions = make([]models.Subscription, len(service.Subscriptions))
		copy(serviceCopy.Subscriptions, service.Subscriptions)
	}

	m.services[key] = &serviceCopy

	return nil
}

// GetService retrieves a single service by its composite key
func (m *ThreadSafeMemoryStore) GetService(ctx context.Context, key string) (*models.ServiceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	service, exists := m.services[key]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", key)
	}

	// Return a deep copy to avoid external mutations
	return m.deepCopyService(service), nil
}

// GetServicesByName retrieves all pods for a given service name
func (m *ThreadSafeMemoryStore) GetServicesByName(ctx context.Context, serviceName string) ([]*models.ServiceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*models.ServiceInfo

	for _, service := range m.services {
		if service.ServiceName == serviceName {
			result = append(result, m.deepCopyService(service))
		}
	}

	return result, nil
}

// GetAllServices retrieves all registered services
func (m *ThreadSafeMemoryStore) GetAllServices(ctx context.Context) ([]*models.ServiceInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*models.ServiceInfo, 0, len(m.services))

	for _, service := range m.services {
		result = append(result, m.deepCopyService(service))
	}

	return result, nil
}

// DeleteService removes a service entry by its composite key
func (m *ThreadSafeMemoryStore) DeleteService(ctx context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.services[key]; !exists {
		return fmt.Errorf("service not found: %s", key)
	}

	delete(m.services, key)
	return nil
}

// UpdateHealthStatus updates the health status and last check timestamp
func (m *ThreadSafeMemoryStore) UpdateHealthStatus(ctx context.Context, key string, status models.ServiceStatus, timestamp time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	service, exists := m.services[key]
	if !exists {
		return fmt.Errorf("service not found: %s", key)
	}

	service.Status = status
	service.LastHealthCheck = timestamp

	return nil
}

// AddSubscription adds a subscriber to a service group
func (m *ThreadSafeMemoryStore) AddSubscription(ctx context.Context, subscriberKey string, serviceGroup string) error {
	if subscriberKey == "" {
		return errors.New("subscriber key cannot be empty")
	}
	if serviceGroup == "" {
		return errors.New("service group cannot be empty")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	subscribers := m.subscriptions[serviceGroup]

	// Check if already subscribed
	for _, sub := range subscribers {
		if sub == subscriberKey {
			return nil // Already subscribed
		}
	}

	// Add new subscription
	m.subscriptions[serviceGroup] = append(subscribers, subscriberKey)
	return nil
}

// RemoveSubscription removes a subscriber from a service group
func (m *ThreadSafeMemoryStore) RemoveSubscription(ctx context.Context, subscriberKey string, serviceGroup string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	subscribers, exists := m.subscriptions[serviceGroup]
	if !exists {
		return nil // No subscriptions for this service group
	}

	// Find and remove the subscriber
	for i, sub := range subscribers {
		if sub == subscriberKey {
			m.subscriptions[serviceGroup] = append(subscribers[:i], subscribers[i+1:]...)

			// Clean up empty subscription lists
			if len(m.subscriptions[serviceGroup]) == 0 {
				delete(m.subscriptions, serviceGroup)
			}

			return nil
		}
	}

	return nil // Subscriber not found, but that's okay
}

// RemoveAllSubscriptions removes all subscriptions for a given subscriber
func (m *ThreadSafeMemoryStore) RemoveAllSubscriptions(ctx context.Context, subscriberKey string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for serviceGroup := range m.subscriptions {
		// Inline the removal logic to avoid nested locking
		subscribers, exists := m.subscriptions[serviceGroup]
		if !exists {
			continue
		}

		// Find and remove the subscriber
		for i, sub := range subscribers {
			if sub == subscriberKey {
				m.subscriptions[serviceGroup] = append(subscribers[:i], subscribers[i+1:]...)

				// Clean up empty subscription lists
				if len(m.subscriptions[serviceGroup]) == 0 {
					delete(m.subscriptions, serviceGroup)
				}

				break
			}
		}
	}

	return nil
}

// GetSubscribers returns all subscriber keys for a given service group
func (m *ThreadSafeMemoryStore) GetSubscribers(ctx context.Context, serviceGroup string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	subscribers, exists := m.subscriptions[serviceGroup]
	if !exists {
		return []string{}, nil
	}

	// Return a copy to avoid external mutations
	result := make([]string, len(subscribers))
	copy(result, subscribers)

	return result, nil
}

// GetSubscriberServices returns full ServiceInfo objects for all subscribers
func (m *ThreadSafeMemoryStore) GetSubscriberServices(ctx context.Context, serviceGroup string) ([]*models.ServiceInfo, error) {
	// Get subscribers with read lock
	subscribers, err := m.GetSubscribers(ctx, serviceGroup)
	if err != nil {
		return nil, err
	}

	// Now get services with read lock
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*models.ServiceInfo, 0, len(subscribers))

	for _, subscriberKey := range subscribers {
		service, exists := m.services[subscriberKey]
		if !exists {
			// Skip services that no longer exist
			continue
		}
		result = append(result, m.deepCopyService(service))
	}

	return result, nil
}

// Close is a no-op for memory storage
func (m *ThreadSafeMemoryStore) Close() error {
	return nil
}

// Ping always succeeds for memory storage
func (m *ThreadSafeMemoryStore) Ping(ctx context.Context) error {
	return nil
}

// Clear removes all data from the store (useful for testing)
func (m *ThreadSafeMemoryStore) Clear() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.services = make(map[string]*models.ServiceInfo)
	m.subscriptions = make(map[string][]string)
}

// GetStats returns statistics about the store (useful for monitoring/debugging)
func (m *ThreadSafeMemoryStore) GetStats() (serviceCount, subscriptionCount int) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return len(m.services), len(m.subscriptions)
}

// deepCopyService creates a deep copy of a service info
func (m *ThreadSafeMemoryStore) deepCopyService(service *models.ServiceInfo) *models.ServiceInfo {
	if service == nil {
		return nil
	}

	serviceCopy := *service

	// Deep copy Providers slice
	if service.Providers != nil {
		serviceCopy.Providers = make([]models.ProviderInfo, len(service.Providers))
		copy(serviceCopy.Providers, service.Providers)
	}

	// Deep copy Subscriptions slice
	if service.Subscriptions != nil {
		serviceCopy.Subscriptions = make([]models.Subscription, len(service.Subscriptions))
		copy(serviceCopy.Subscriptions, service.Subscriptions)
	}

	return &serviceCopy
}
