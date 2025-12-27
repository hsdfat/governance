package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditStore implements storage.AuditStore using MongoDB
type AuditStore struct {
	client     *mongo.Client
	database   *mongo.Database
	collection *mongo.Collection
}

// Ensure AuditStore implements storage.AuditStore
var _ storage.AuditStore = (*AuditStore)(nil)

// NewAuditStore creates a new MongoDB audit store and initializes indexes
func NewAuditStore(cfg Config) (*AuditStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(cfg.URI)

	// Set connection pool settings
	if cfg.MaxPoolSize > 0 {
		clientOpts.SetMaxPoolSize(cfg.MaxPoolSize)
	}
	if cfg.MinPoolSize > 0 {
		clientOpts.SetMinPoolSize(cfg.MinPoolSize)
	}

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to MongoDB: %w", err)
	}

	// Ping to verify connection
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	database := client.Database(cfg.Database)
	collection := database.Collection("audit_logs")

	store := &AuditStore{
		client:     client,
		database:   database,
		collection: collection,
	}

	// Create indexes
	if err := store.createIndexes(ctx); err != nil {
		return nil, fmt.Errorf("failed to create indexes: %w", err)
	}

	return store, nil
}

// createIndexes creates indexes for common query patterns
func (a *AuditStore) createIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "timestamp", Value: -1}},
			Options: options.Index().SetName("idx_timestamp"),
		},
		{
			Keys:    bson.D{{Key: "service_name", Value: 1}},
			Options: options.Index().SetName("idx_service_name"),
		},
		{
			Keys:    bson.D{{Key: "action", Value: 1}},
			Options: options.Index().SetName("idx_action"),
		},
		{
			Keys:    bson.D{{Key: "result", Value: 1}},
			Options: options.Index().SetName("idx_result"),
		},
		{
			Keys: bson.D{
				{Key: "service_name", Value: 1},
				{Key: "pod_name", Value: 1},
			},
			Options: options.Index().SetName("idx_service_pod"),
		},
	}

	_, err := a.collection.Indexes().CreateMany(ctx, indexes)
	return err
}

// SaveAuditLog stores a new audit log entry
func (a *AuditStore) SaveAuditLog(ctx context.Context, log *models.AuditLog) error {
	// Generate ID if not provided
	if log.ID == "" {
		log.ID = uuid.New().String()
	}

	// Set timestamp if not provided
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}

	_, err := a.collection.InsertOne(ctx, log)
	if err != nil {
		return fmt.Errorf("failed to insert audit log: %w", err)
	}

	return nil
}

// GetAuditLogs retrieves audit logs based on filter criteria
func (a *AuditStore) GetAuditLogs(ctx context.Context, filter *models.AuditLogFilter) ([]models.AuditLog, int, error) {
	// Build filter
	bsonFilter := bson.M{}

	if filter != nil {
		if filter.StartTime != nil {
			bsonFilter["timestamp"] = bson.M{"$gte": *filter.StartTime}
		}

		if filter.EndTime != nil {
			if existingTime, ok := bsonFilter["timestamp"]; ok {
				bsonFilter["timestamp"] = bson.M{
					"$gte": existingTime.(bson.M)["$gte"],
					"$lte": *filter.EndTime,
				}
			} else {
				bsonFilter["timestamp"] = bson.M{"$lte": *filter.EndTime}
			}
		}

		if len(filter.Actions) > 0 {
			bsonFilter["action"] = bson.M{"$in": filter.Actions}
		}

		if len(filter.Results) > 0 {
			bsonFilter["result"] = bson.M{"$in": filter.Results}
		}

		if filter.ServiceName != nil {
			bsonFilter["service_name"] = *filter.ServiceName
		}

		if filter.PodName != nil {
			bsonFilter["pod_name"] = *filter.PodName
		}
	}

	// Get total count
	total, err := a.collection.CountDocuments(ctx, bsonFilter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Set default pagination
	limit := int64(100)
	offset := int64(0)
	if filter != nil {
		if filter.Limit > 0 {
			limit = int64(filter.Limit)
		}
		if filter.Offset > 0 {
			offset = int64(filter.Offset)
		}
	}

	// Build query with pagination
	findOptions := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit).
		SetSkip(offset)

	cursor, err := a.collection.Find(ctx, bsonFilter, findOptions)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer cursor.Close(ctx)

	logs := []models.AuditLog{}
	if err := cursor.All(ctx, &logs); err != nil {
		return nil, 0, fmt.Errorf("failed to decode audit logs: %w", err)
	}

	return logs, int(total), nil
}

// GetAuditLogByID retrieves a specific audit log by its ID
func (a *AuditStore) GetAuditLogByID(ctx context.Context, id string) (*models.AuditLog, error) {
	filter := bson.M{"id": id}

	var log models.AuditLog
	err := a.collection.FindOne(ctx, filter).Decode(&log)
	if err == mongo.ErrNoDocuments {
		return nil, fmt.Errorf("audit log not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	return &log, nil
}

// DeleteOldAuditLogs deletes audit logs older than the specified retention period
func (a *AuditStore) DeleteOldAuditLogs(ctx context.Context, retentionDays int) (int, error) {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	filter := bson.M{"timestamp": bson.M{"$lt": cutoffTime}}

	result, err := a.collection.DeleteMany(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old audit logs: %w", err)
	}

	return int(result.DeletedCount), nil
}

// Close closes the MongoDB connection
func (a *AuditStore) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.client.Disconnect(ctx)
}

// Ping checks if the MongoDB is accessible
func (a *AuditStore) Ping(ctx context.Context) error {
	return a.client.Ping(ctx, nil)
}
