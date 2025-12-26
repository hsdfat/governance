package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/chronnie/governance/models"
)

// inMemoryCache is a simple in-memory cache embedded in DualStore
type inMemoryCache struct {
	services      map[string]*models.ServiceInfo
	subscriptions map[string][]string
}

func newInMemoryCache() *inMemoryCache {
	return &inMemoryCache{
		services:      make(map[string]*models.ServiceInfo),
		subscriptions: make(map[string][]string),
	}
}

// Cache methods

func (c *inMemoryCache) SaveService(ctx context.Context, service *models.ServiceInfo) error {
	if service == nil {
		return fmt.Errorf("service cannot be nil")
	}
	key := service.GetKey()
	if key == "" {
		return fmt.Errorf("service key cannot be empty")
	}
	serviceCopy := *service
	c.services[key] = &serviceCopy
	return nil
}

func (c *inMemoryCache) GetService(ctx context.Context, key string) (*models.ServiceInfo, error) {
	service, exists := c.services[key]
	if !exists {
		return nil, fmt.Errorf("service not found: %s", key)
	}
	serviceCopy := *service
	return &serviceCopy, nil
}

func (c *inMemoryCache) GetServicesByName(ctx context.Context, serviceName string) ([]*models.ServiceInfo, error) {
	var result []*models.ServiceInfo
	for _, service := range c.services {
		if service.ServiceName == serviceName {
			serviceCopy := *service
			result = append(result, &serviceCopy)
		}
	}
	return result, nil
}

func (c *inMemoryCache) GetAllServices(ctx context.Context) ([]*models.ServiceInfo, error) {
	result := make([]*models.ServiceInfo, 0, len(c.services))
	for _, service := range c.services {
		serviceCopy := *service
		result = append(result, &serviceCopy)
	}
	return result, nil
}

func (c *inMemoryCache) DeleteService(ctx context.Context, key string) error {
	if _, exists := c.services[key]; !exists {
		return fmt.Errorf("service not found: %s", key)
	}
	delete(c.services, key)
	return nil
}

func (c *inMemoryCache) UpdateHealthStatus(ctx context.Context, key string, status models.ServiceStatus, timestamp time.Time) error {
	service, exists := c.services[key]
	if !exists {
		return fmt.Errorf("service not found: %s", key)
	}
	service.Status = status
	service.LastHealthCheck = timestamp
	return nil
}

func (c *inMemoryCache) AddSubscription(ctx context.Context, subscriberKey string, serviceGroup string) error {
	if subscriberKey == "" {
		return fmt.Errorf("subscriber key cannot be empty")
	}
	if serviceGroup == "" {
		return fmt.Errorf("service group cannot be empty")
	}

	subscribers := c.subscriptions[serviceGroup]
	for _, sub := range subscribers {
		if sub == subscriberKey {
			return nil // Already subscribed
		}
	}
	c.subscriptions[serviceGroup] = append(subscribers, subscriberKey)
	return nil
}

func (c *inMemoryCache) RemoveSubscription(ctx context.Context, subscriberKey string, serviceGroup string) error {
	subscribers, exists := c.subscriptions[serviceGroup]
	if !exists {
		return nil
	}

	for i, sub := range subscribers {
		if sub == subscriberKey {
			c.subscriptions[serviceGroup] = append(subscribers[:i], subscribers[i+1:]...)
			if len(c.subscriptions[serviceGroup]) == 0 {
				delete(c.subscriptions, serviceGroup)
			}
			return nil
		}
	}
	return nil
}

func (c *inMemoryCache) RemoveAllSubscriptions(ctx context.Context, subscriberKey string) error {
	for serviceGroup := range c.subscriptions {
		c.RemoveSubscription(ctx, subscriberKey, serviceGroup)
	}
	return nil
}

func (c *inMemoryCache) GetSubscribers(ctx context.Context, serviceGroup string) ([]string, error) {
	subscribers, exists := c.subscriptions[serviceGroup]
	if !exists {
		return []string{}, nil
	}
	result := make([]string, len(subscribers))
	copy(result, subscribers)
	return result, nil
}

func (c *inMemoryCache) GetSubscriberServices(ctx context.Context, serviceGroup string) ([]*models.ServiceInfo, error) {
	subscribers, err := c.GetSubscribers(ctx, serviceGroup)
	if err != nil {
		return nil, err
	}

	result := make([]*models.ServiceInfo, 0, len(subscribers))
	for _, subscriberKey := range subscribers {
		service, err := c.GetService(ctx, subscriberKey)
		if err != nil {
			continue // Skip services that no longer exist
		}
		result = append(result, service)
	}
	return result, nil
}

// DualStore combines in-memory cache with optional database persistence.
// All reads/writes go to memory for performance.
// Database writes happen asynchronously (fire-and-forget).
type DualStore struct {
	cache *inMemoryCache
	db    DatabaseStore // nil if database persistence is disabled
}

// Ensure DualStore implements RegistryStore
var _ RegistryStore = (*DualStore)(nil)

// NewDualStore creates a new dual-layer storage.
// If db is nil, only in-memory cache is used (no persistence).
func NewDualStore(db DatabaseStore) *DualStore {
	return &DualStore{
		cache: newInMemoryCache(),
		db:    db,
	}
}

// GetDatabase returns the underlying database store (may be nil)
func (d *DualStore) GetDatabase() DatabaseStore {
	return d.db
}

// SaveService stores to cache immediately, then persists to database asynchronously
func (d *DualStore) SaveService(ctx context.Context, service *models.ServiceInfo) error {
	// Always save to cache first (synchronous)
	if err := d.cache.SaveService(ctx, service); err != nil {
		return err
	}

	// Persist to database asynchronously if enabled
	if d.db != nil {
		go d.db.SaveService(context.Background(), service)
	}

	return nil
}

// GetService retrieves from cache (fast)
func (d *DualStore) GetService(ctx context.Context, key string) (*models.ServiceInfo, error) {
	return d.cache.GetService(ctx, key)
}

// GetServicesByName retrieves from cache (fast)
func (d *DualStore) GetServicesByName(ctx context.Context, serviceName string) ([]*models.ServiceInfo, error) {
	return d.cache.GetServicesByName(ctx, serviceName)
}

// GetAllServices retrieves from cache (fast)
func (d *DualStore) GetAllServices(ctx context.Context) ([]*models.ServiceInfo, error) {
	return d.cache.GetAllServices(ctx)
}

// DeleteService deletes from cache immediately, then from database asynchronously
func (d *DualStore) DeleteService(ctx context.Context, key string) error {
	// Always delete from cache first (synchronous)
	if err := d.cache.DeleteService(ctx, key); err != nil {
		return err
	}

	// Delete from database asynchronously if enabled
	if d.db != nil {
		go d.db.DeleteService(context.Background(), key)
	}

	return nil
}

// UpdateHealthStatus updates cache immediately, then database asynchronously
func (d *DualStore) UpdateHealthStatus(ctx context.Context, key string, status models.ServiceStatus, timestamp time.Time) error {
	// Always update cache first (synchronous)
	if err := d.cache.UpdateHealthStatus(ctx, key, status, timestamp); err != nil {
		return err
	}

	// Update database asynchronously if enabled
	if d.db != nil {
		go d.db.UpdateHealthStatus(context.Background(), key, status, timestamp)
	}

	return nil
}

// AddSubscription adds to cache immediately, then persists to database asynchronously
func (d *DualStore) AddSubscription(ctx context.Context, subscriberKey string, serviceGroup string) error {
	// Always add to cache first (synchronous)
	if err := d.cache.AddSubscription(ctx, subscriberKey, serviceGroup); err != nil {
		return err
	}

	// Persist to database asynchronously if enabled
	// Note: We need to save all subscriptions, so fetch from cache first
	if d.db != nil {
		go func() {
			subscribers, _ := d.cache.GetSubscribers(context.Background(), serviceGroup)
			// For simplicity, we could store subscriptions differently
			// For now, this is a placeholder - actual implementation depends on DB schema
			_ = subscribers
		}()
	}

	return nil
}

// RemoveSubscription removes from cache immediately, then from database asynchronously
func (d *DualStore) RemoveSubscription(ctx context.Context, subscriberKey string, serviceGroup string) error {
	// Always remove from cache first (synchronous)
	if err := d.cache.RemoveSubscription(ctx, subscriberKey, serviceGroup); err != nil {
		return err
	}

	// Update database asynchronously if enabled
	if d.db != nil {
		go func() {
			// Similar to AddSubscription, actual implementation depends on DB schema
		}()
	}

	return nil
}

// RemoveAllSubscriptions removes from cache immediately, then from database asynchronously
func (d *DualStore) RemoveAllSubscriptions(ctx context.Context, subscriberKey string) error {
	// Always remove from cache first (synchronous)
	if err := d.cache.RemoveAllSubscriptions(ctx, subscriberKey); err != nil {
		return err
	}

	// Delete from database asynchronously if enabled
	if d.db != nil {
		go d.db.DeleteSubscriptions(context.Background(), subscriberKey)
	}

	return nil
}

// GetSubscribers retrieves from cache (fast)
func (d *DualStore) GetSubscribers(ctx context.Context, serviceGroup string) ([]string, error) {
	return d.cache.GetSubscribers(ctx, serviceGroup)
}

// GetSubscriberServices retrieves from cache (fast)
func (d *DualStore) GetSubscriberServices(ctx context.Context, serviceGroup string) ([]*models.ServiceInfo, error) {
	return d.cache.GetSubscriberServices(ctx, serviceGroup)
}

// Close closes the database connection (cache doesn't need closing)
func (d *DualStore) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// Ping checks database health (cache is always healthy)
func (d *DualStore) Ping(ctx context.Context) error {
	if d.db != nil {
		return d.db.Ping(ctx)
	}
	return nil
}

// SyncFromDatabase loads all data from database into cache.
// This is called during reconciliation to ensure cache and database are in sync.
// Returns the number of services and subscriptions synced.
func (d *DualStore) SyncFromDatabase(ctx context.Context) (servicesSynced int, subscriptionsSynced int, err error) {
	if d.db == nil {
		return 0, 0, nil // No database, nothing to sync
	}

	// Load all services from database
	services, err := d.db.GetAllServices(ctx)
	if err != nil {
		return 0, 0, err
	}

	// Update cache with database data
	for _, service := range services {
		d.cache.SaveService(ctx, service)
	}
	servicesSynced = len(services)

	// Load all subscriptions from database
	allSubs, err := d.db.GetAllSubscriptions(ctx)
	if err != nil {
		return servicesSynced, 0, err
	}

	// Update cache with subscription data
	for subscriberKey, serviceGroups := range allSubs {
		for _, serviceGroup := range serviceGroups {
			d.cache.AddSubscription(ctx, subscriberKey, serviceGroup)
		}
		subscriptionsSynced += len(serviceGroups)
	}

	return servicesSynced, subscriptionsSynced, nil
}

// SyncToDatabase writes all cache data to database.
// This is useful for initial database population or full sync.
func (d *DualStore) SyncToDatabase(ctx context.Context) error {
	if d.db == nil {
		return nil // No database, nothing to sync
	}

	// Get all services from cache
	services, err := d.cache.GetAllServices(ctx)
	if err != nil {
		return err
	}

	// Write to database
	for _, service := range services {
		if err := d.db.SaveService(ctx, service); err != nil {
			return err
		}

		// Also save subscriptions (convert to service names only for backward compatibility)
		if len(service.Subscriptions) > 0 {
			key := service.GetKey()
			serviceNames := make([]string, len(service.Subscriptions))
			for i, sub := range service.Subscriptions {
				serviceNames[i] = sub.ServiceName
			}
			if err := d.db.SaveSubscriptions(ctx, key, serviceNames); err != nil {
				return err
			}
		}
	}

	return nil
}
