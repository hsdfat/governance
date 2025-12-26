package mongodb

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage"
)

// Config holds MongoDB connection configuration
type Config struct {
	URI            string        // MongoDB connection URI (e.g., mongodb://localhost:27017)
	Database       string        // Database name
	ConnectTimeout time.Duration // Connection timeout
	// Optional parameters
	MaxPoolSize uint64
	MinPoolSize uint64
}

// DatabaseStore implements storage.DatabaseStore using MongoDB
type DatabaseStore struct {
	client             *mongo.Client
	database           *mongo.Database
	servicesCollection *mongo.Collection
}

// Ensure DatabaseStore implements storage.DatabaseStore
var _ storage.DatabaseStore = (*DatabaseStore)(nil)

// serviceDoc represents the MongoDB document structure for services
type serviceDoc struct {
	ServiceKey      string                `bson:"_id"`
	ServiceName     string                `bson:"service_name"`
	PodName         string                `bson:"pod_name"`
	Providers       []models.ProviderInfo `bson:"providers"`
	HealthCheckURL  string                `bson:"health_check_url"`
	NotificationURL string                `bson:"notification_url"`
	Subscriptions   []string              `bson:"subscriptions"`
	Status          models.ServiceStatus  `bson:"status"`
	LastHealthCheck time.Time             `bson:"last_health_check"`
	RegisteredAt    time.Time             `bson:"registered_at"`
	UpdatedAt       time.Time             `bson:"updated_at"`
}

// NewDatabaseStore creates a new MongoDB database store and initializes collections
func NewDatabaseStore(cfg Config) (*DatabaseStore, error) {
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}

	clientOpts := options.Client().ApplyURI(cfg.URI)

	if cfg.MaxPoolSize > 0 {
		clientOpts.SetMaxPoolSize(cfg.MaxPoolSize)
	}

	if cfg.MinPoolSize > 0 {
		clientOpts.SetMinPoolSize(cfg.MinPoolSize)
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ConnectTimeout)
	defer cancel()

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	database := client.Database(cfg.Database)
	servicesCollection := database.Collection("services")

	store := &DatabaseStore{
		client:             client,
		database:           database,
		servicesCollection: servicesCollection,
	}

	// Create indexes
	if err := store.createIndexes(context.Background()); err != nil {
		client.Disconnect(context.Background())
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	return store, nil
}

// createIndexes creates necessary indexes for optimal query performance
func (d *DatabaseStore) createIndexes(ctx context.Context) error {
	servicesIndexes := []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "service_name", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "status", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "updated_at", Value: 1}},
		},
	}

	if _, err := d.servicesCollection.Indexes().CreateMany(ctx, servicesIndexes); err != nil {
		return fmt.Errorf("failed to create services indexes: %w", err)
	}

	return nil
}

// toServiceDoc converts ServiceInfo to serviceDoc
func toServiceDoc(service *models.ServiceInfo) *serviceDoc {
	// Convert Subscription structs to service names for storage
	subNames := make([]string, len(service.Subscriptions))
	for i, sub := range service.Subscriptions {
		subNames[i] = sub.ServiceName
	}

	return &serviceDoc{
		ServiceKey:      service.GetKey(),
		ServiceName:     service.ServiceName,
		PodName:         service.PodName,
		Providers:       service.Providers,
		HealthCheckURL:  service.HealthCheckURL,
		NotificationURL: service.NotificationURL,
		Subscriptions:   subNames,
		Status:          service.Status,
		LastHealthCheck: service.LastHealthCheck,
		RegisteredAt:    service.RegisteredAt,
		UpdatedAt:       time.Now(),
	}
}

// toServiceInfo converts serviceDoc to ServiceInfo
func (doc *serviceDoc) toServiceInfo() *models.ServiceInfo {
	// Convert service names back to Subscription structs
	subs := make([]models.Subscription, len(doc.Subscriptions))
	for i, serviceName := range doc.Subscriptions {
		subs[i] = models.Subscription{ServiceName: serviceName}
	}

	return &models.ServiceInfo{
		ServiceName:     doc.ServiceName,
		PodName:         doc.PodName,
		Providers:       doc.Providers,
		HealthCheckURL:  doc.HealthCheckURL,
		NotificationURL: doc.NotificationURL,
		Subscriptions:   subs,
		Status:          doc.Status,
		LastHealthCheck: doc.LastHealthCheck,
		RegisteredAt:    doc.RegisteredAt,
	}
}

// SaveService stores or updates a service entry
func (d *DatabaseStore) SaveService(ctx context.Context, service *models.ServiceInfo) error {
	if service == nil {
		return fmt.Errorf("service cannot be nil")
	}

	key := service.GetKey()
	if key == "" {
		return fmt.Errorf("service key cannot be empty")
	}

	doc := toServiceDoc(service)

	opts := options.Replace().SetUpsert(true)
	_, err := d.servicesCollection.ReplaceOne(
		ctx,
		bson.M{"_id": key},
		doc,
		opts,
	)

	if err != nil {
		return fmt.Errorf("failed to save service: %w", err)
	}

	return nil
}

// GetService retrieves a single service by its composite key
func (d *DatabaseStore) GetService(ctx context.Context, key string) (*models.ServiceInfo, error) {
	var doc serviceDoc

	err := d.servicesCollection.FindOne(ctx, bson.M{"_id": key}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("service not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	return doc.toServiceInfo(), nil
}

// GetAllServices retrieves all registered services
func (d *DatabaseStore) GetAllServices(ctx context.Context) ([]*models.ServiceInfo, error) {
	opts := options.Find().SetSort(bson.D{
		{Key: "service_name", Value: 1},
		{Key: "pod_name", Value: 1},
	})

	cursor, err := d.servicesCollection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, fmt.Errorf("failed to query services: %w", err)
	}
	defer cursor.Close(ctx)

	var result []*models.ServiceInfo

	for cursor.Next(ctx) {
		var doc serviceDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode service: %w", err)
		}
		result = append(result, doc.toServiceInfo())
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return result, nil
}

// DeleteService removes a service entry by its composite key
func (d *DatabaseStore) DeleteService(ctx context.Context, key string) error {
	result, err := d.servicesCollection.DeleteOne(ctx, bson.M{"_id": key})
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	if result.DeletedCount == 0 {
		return fmt.Errorf("service not found: %s", key)
	}

	return nil
}

// UpdateHealthStatus updates the health status and last check timestamp
func (d *DatabaseStore) UpdateHealthStatus(ctx context.Context, key string, status models.ServiceStatus, timestamp time.Time) error {
	update := bson.M{
		"$set": bson.M{
			"status":            status,
			"last_health_check": timestamp,
			"updated_at":        time.Now(),
		},
	}

	result, err := d.servicesCollection.UpdateOne(ctx, bson.M{"_id": key}, update)
	if err != nil {
		return fmt.Errorf("failed to update health status: %w", err)
	}

	if result.MatchedCount == 0 {
		return fmt.Errorf("service not found: %s", key)
	}

	return nil
}

// SaveSubscriptions saves all subscriptions for a service (replaces existing)
func (d *DatabaseStore) SaveSubscriptions(ctx context.Context, subscriberKey string, subscriptions []string) error {
	// For MongoDB, subscriptions are stored in the service document
	// So this is handled by SaveService
	return nil
}

// GetSubscriptions retrieves all service groups that a subscriber is subscribed to
func (d *DatabaseStore) GetSubscriptions(ctx context.Context, subscriberKey string) ([]string, error) {
	var doc serviceDoc

	err := d.servicesCollection.FindOne(ctx, bson.M{"_id": subscriberKey}).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}

	return doc.Subscriptions, nil
}

// GetAllSubscriptions retrieves all subscription relationships
func (d *DatabaseStore) GetAllSubscriptions(ctx context.Context) (map[string][]string, error) {
	cursor, err := d.servicesCollection.Find(ctx, bson.M{})
	if err != nil {
		return nil, fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer cursor.Close(ctx)

	result := make(map[string][]string)

	for cursor.Next(ctx) {
		var doc serviceDoc
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("failed to decode service: %w", err)
		}

		if len(doc.Subscriptions) > 0 {
			result[doc.ServiceKey] = doc.Subscriptions
		}
	}

	if err := cursor.Err(); err != nil {
		return nil, fmt.Errorf("cursor error: %w", err)
	}

	return result, nil
}

// DeleteSubscriptions removes all subscriptions for a subscriber
func (d *DatabaseStore) DeleteSubscriptions(ctx context.Context, subscriberKey string) error {
	// For MongoDB, subscriptions are stored in the service document
	// This is handled when the service is deleted
	return nil
}

// Close closes the MongoDB connection
func (d *DatabaseStore) Close() error {
	if d.client != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return d.client.Disconnect(ctx)
	}
	return nil
}

// Ping checks if the database is accessible
func (d *DatabaseStore) Ping(ctx context.Context) error {
	return d.client.Ping(ctx, nil)
}
