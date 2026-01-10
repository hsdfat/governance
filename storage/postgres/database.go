package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage"
)

// Config holds PostgreSQL connection configuration
type Config struct {
	Host     string
	Port     int
	Database string
	Username string
	Password string
	SSLMode  string // disable, require, verify-ca, verify-full
	// Optional parameters
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

// DatabaseStore implements storage.DatabaseStore using PostgreSQL
type DatabaseStore struct {
	db *sql.DB
}

// Ensure DatabaseStore implements storage.DatabaseStore
var _ storage.DatabaseStore = (*DatabaseStore)(nil)

// NewDatabaseStore creates a new PostgreSQL database store and initializes tables
func NewDatabaseStore(cfg Config) (*DatabaseStore, error) {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	// Use URL format for DSN (better compatibility with YugabyteDB than key=value format)
	passwordPart := ""
	if cfg.Password != "" {
		passwordPart = ":" + cfg.Password
	}

	// First, connect to the default database to check if target database exists
	defaultDB := "yugabyte" // YugabyteDB default database
	defaultDSN := fmt.Sprintf("postgres://%s%s@%s:%d/%s?sslmode=%s",
		cfg.Username, passwordPart, cfg.Host, cfg.Port, defaultDB, sslMode)

	adminDB, err := sql.Open("postgres", defaultDSN)
	if err != nil {
		return nil, fmt.Errorf("failed to open admin connection: %w", err)
	}
	defer adminDB.Close()

	// Test admin connection
	if err := adminDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping admin database: %w", err)
	}

	// Check if target database exists
	var exists bool
	checkQuery := "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)"
	if err := adminDB.QueryRow(checkQuery, cfg.Database).Scan(&exists); err != nil {
		return nil, fmt.Errorf("failed to check database existence: %w", err)
	}

	// Create database if it doesn't exist
	if !exists {
		createQuery := fmt.Sprintf("CREATE DATABASE %s", cfg.Database)
		if _, err := adminDB.Exec(createQuery); err != nil {
			return nil, fmt.Errorf("failed to create database %s: %w", cfg.Database, err)
		}
	}

	// Now connect to the target database
	dsn := fmt.Sprintf("postgres://%s%s@%s:%d/%s?sslmode=%s",
		cfg.Username, passwordPart, cfg.Host, cfg.Port, cfg.Database, sslMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Test the connection and verify we're connected to the correct database
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	// Verify we're connected to the correct database (important for YugabyteDB)
	var actualDB string
	if err := db.QueryRow("SELECT current_database()").Scan(&actualDB); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to verify database: %w", err)
	}
	if actualDB != cfg.Database {
		db.Close()
		return nil, fmt.Errorf("connected to wrong database: expected=%s, actual=%s", cfg.Database, actualDB)
	}

	// Set connection pool settings
	if cfg.MaxOpenConns > 0 {
		db.SetMaxOpenConns(cfg.MaxOpenConns)
	} else {
		db.SetMaxOpenConns(25)
	}

	if cfg.MaxIdleConns > 0 {
		db.SetMaxIdleConns(cfg.MaxIdleConns)
	} else {
		db.SetMaxIdleConns(5)
	}

	if cfg.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	} else {
		db.SetConnMaxLifetime(5 * time.Minute)
	}

	store := &DatabaseStore{db: db}

	// Initialize tables
	if err := store.initTables(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	return store, nil
}

// initTables creates the necessary database tables if they don't exist
func (d *DatabaseStore) initTables(ctx context.Context) error {
	queries := []string{
		// Services table
		`CREATE TABLE IF NOT EXISTS services (
			service_key VARCHAR(255) PRIMARY KEY,
			service_name VARCHAR(128) NOT NULL,
			pod_name VARCHAR(128) NOT NULL,
			providers JSONB NOT NULL,
			health_check_url VARCHAR(512) NOT NULL,
			notification_url VARCHAR(512) NOT NULL,
			subscriptions JSONB NOT NULL,
			status VARCHAR(20) NOT NULL,
			last_health_check TIMESTAMP NOT NULL,
			registered_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,

		// Create indexes for services table
		`CREATE INDEX IF NOT EXISTS idx_services_service_name ON services(service_name)`,
		`CREATE INDEX IF NOT EXISTS idx_services_status ON services(status)`,
	}

	for _, query := range queries {
		if _, err := d.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("failed to execute query: %w", err)
		}
	}

	return nil
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

	// Marshal JSON fields
	providersJSON, err := json.Marshal(service.Providers)
	if err != nil {
		return fmt.Errorf("failed to marshal providers: %w", err)
	}

	subscriptionsJSON, err := json.Marshal(service.Subscriptions)
	if err != nil {
		return fmt.Errorf("failed to marshal subscriptions: %w", err)
	}

	query := `INSERT INTO services
		(service_key, service_name, pod_name, providers, health_check_url, notification_url,
		 subscriptions, status, last_health_check, registered_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, CURRENT_TIMESTAMP)
		ON CONFLICT (service_key) DO UPDATE SET
		providers = EXCLUDED.providers,
		health_check_url = EXCLUDED.health_check_url,
		notification_url = EXCLUDED.notification_url,
		subscriptions = EXCLUDED.subscriptions,
		status = EXCLUDED.status,
		last_health_check = EXCLUDED.last_health_check,
		updated_at = CURRENT_TIMESTAMP`

	result, err := d.db.ExecContext(ctx, query,
		key, service.ServiceName, service.PodName,
		providersJSON, service.HealthCheckURL, service.NotificationURL,
		subscriptionsJSON, service.Status, service.LastHealthCheck, service.RegisteredAt)

	if err != nil {
		return fmt.Errorf("failed to save service: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected != 1 {
		return fmt.Errorf("expected 1 row affected, got %d", rowsAffected)
	}

	return nil
}

// GetService retrieves a single service by its composite key
func (d *DatabaseStore) GetService(ctx context.Context, key string) (*models.ServiceInfo, error) {
	query := `SELECT service_name, pod_name, providers, health_check_url, notification_url,
		subscriptions, status, last_health_check, registered_at
		FROM services WHERE service_key = $1`

	var service models.ServiceInfo
	var providersJSON, subscriptionsJSON []byte

	err := d.db.QueryRowContext(ctx, query, key).Scan(
		&service.ServiceName, &service.PodName,
		&providersJSON, &service.HealthCheckURL, &service.NotificationURL,
		&subscriptionsJSON, &service.Status, &service.LastHealthCheck, &service.RegisteredAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("service not found: %s", key)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get service: %w", err)
	}

	// Unmarshal JSON fields
	if err := json.Unmarshal(providersJSON, &service.Providers); err != nil {
		return nil, fmt.Errorf("failed to unmarshal providers: %w", err)
	}

	if err := json.Unmarshal(subscriptionsJSON, &service.Subscriptions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subscriptions: %w", err)
	}

	return &service, nil
}

// GetAllServices retrieves all registered services
func (d *DatabaseStore) GetAllServices(ctx context.Context) ([]*models.ServiceInfo, error) {
	query := `SELECT service_name, pod_name, providers, health_check_url, notification_url,
		subscriptions, status, last_health_check, registered_at
		FROM services
		ORDER BY service_name, pod_name`

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query services: %w", err)
	}
	defer rows.Close()

	var result []*models.ServiceInfo

	for rows.Next() {
		var service models.ServiceInfo
		var providersJSON, subscriptionsJSON []byte

		err := rows.Scan(
			&service.ServiceName, &service.PodName,
			&providersJSON, &service.HealthCheckURL, &service.NotificationURL,
			&subscriptionsJSON, &service.Status, &service.LastHealthCheck, &service.RegisteredAt)

		if err != nil {
			return nil, fmt.Errorf("failed to scan service: %w", err)
		}

		// Unmarshal JSON fields
		if err := json.Unmarshal(providersJSON, &service.Providers); err != nil {
			return nil, fmt.Errorf("failed to unmarshal providers: %w", err)
		}

		if err := json.Unmarshal(subscriptionsJSON, &service.Subscriptions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal subscriptions: %w", err)
		}

		result = append(result, &service)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// DeleteService removes a service entry by its composite key
func (d *DatabaseStore) DeleteService(ctx context.Context, key string) error {
	query := `DELETE FROM services WHERE service_key = $1`

	result, err := d.db.ExecContext(ctx, query, key)
	if err != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("service not found: %s", key)
	}

	return nil
}

// UpdateHealthStatus updates the health status and last check timestamp
func (d *DatabaseStore) UpdateHealthStatus(ctx context.Context, key string, status models.ServiceStatus, timestamp time.Time) error {
	query := `UPDATE services SET status = $1, last_health_check = $2, updated_at = CURRENT_TIMESTAMP WHERE service_key = $3`

	result, err := d.db.ExecContext(ctx, query, status, timestamp, key)
	if err != nil {
		return fmt.Errorf("failed to update health status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("service not found: %s", key)
	}

	return nil
}

// SaveSubscriptions saves all subscriptions for a service (replaces existing)
func (d *DatabaseStore) SaveSubscriptions(ctx context.Context, subscriberKey string, subscriptions []string) error {
	// For PostgreSQL, we store subscriptions in the services table as JSONB
	// So this is handled by SaveService
	return nil
}

// GetSubscriptions retrieves all service groups that a subscriber is subscribed to
func (d *DatabaseStore) GetSubscriptions(ctx context.Context, subscriberKey string) ([]string, error) {
	query := `SELECT subscriptions FROM services WHERE service_key = $1`

	var subscriptionsJSON []byte
	err := d.db.QueryRowContext(ctx, query, subscriberKey).Scan(&subscriptionsJSON)

	if err == sql.ErrNoRows {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get subscriptions: %w", err)
	}

	var subscriptions []string
	if err := json.Unmarshal(subscriptionsJSON, &subscriptions); err != nil {
		return nil, fmt.Errorf("failed to unmarshal subscriptions: %w", err)
	}

	return subscriptions, nil
}

// GetAllSubscriptions retrieves all subscription relationships
func (d *DatabaseStore) GetAllSubscriptions(ctx context.Context) (map[string][]string, error) {
	query := `SELECT service_key, subscriptions FROM services`

	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query subscriptions: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)

	for rows.Next() {
		var subscriberKey string
		var subscriptionsJSON []byte

		if err := rows.Scan(&subscriberKey, &subscriptionsJSON); err != nil {
			return nil, fmt.Errorf("failed to scan subscription: %w", err)
		}

		var subscriptions []string
		if err := json.Unmarshal(subscriptionsJSON, &subscriptions); err != nil {
			return nil, fmt.Errorf("failed to unmarshal subscriptions: %w", err)
		}

		if len(subscriptions) > 0 {
			result[subscriberKey] = subscriptions
		}
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return result, nil
}

// DeleteSubscriptions removes all subscriptions for a subscriber
func (d *DatabaseStore) DeleteSubscriptions(ctx context.Context, subscriberKey string) error {
	// For PostgreSQL, subscriptions are stored in the services table
	// This is handled when the service is deleted
	return nil
}

// Close closes the database connection
func (d *DatabaseStore) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

// Ping checks if the database is accessible
func (d *DatabaseStore) Ping(ctx context.Context) error {
	return d.db.PingContext(ctx)
}
