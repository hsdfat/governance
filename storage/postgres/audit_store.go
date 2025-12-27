package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage"
	"github.com/google/uuid"
)

// AuditStore implements storage.AuditStore using PostgreSQL
type AuditStore struct {
	db *sql.DB
}

// Ensure AuditStore implements storage.AuditStore
var _ storage.AuditStore = (*AuditStore)(nil)

// NewAuditStore creates a new PostgreSQL audit store and initializes tables
func NewAuditStore(cfg Config) (*AuditStore, error) {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.Database, sslMode)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
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

	store := &AuditStore{db: db}

	// Initialize tables
	if err := store.initTables(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize tables: %w", err)
	}

	return store, nil
}

// initTables creates the audit_logs table if it doesn't exist
func (a *AuditStore) initTables(ctx context.Context) error {
	query := `CREATE TABLE IF NOT EXISTS audit_logs (
		id VARCHAR(36) PRIMARY KEY,
		timestamp TIMESTAMP NOT NULL,
		action VARCHAR(50) NOT NULL,
		result VARCHAR(20) NOT NULL,
		service_name VARCHAR(128) NOT NULL,
		pod_name VARCHAR(128) NOT NULL,
		details JSONB,
		error_msg TEXT,
		previous_status VARCHAR(20),
		new_status VARCHAR(20),
		consecutive_failures INTEGER,
		failure_reason TEXT,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`

	// Create indexes for common query patterns
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_service ON audit_logs(service_name)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_action ON audit_logs(action)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_result ON audit_logs(result)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_service_pod ON audit_logs(service_name, pod_name)`,
	}

	// Execute table creation
	if _, err := a.db.ExecContext(ctx, query); err != nil {
		return fmt.Errorf("failed to create audit_logs table: %w", err)
	}

	// Execute index creation
	for _, indexQuery := range indexes {
		if _, err := a.db.ExecContext(ctx, indexQuery); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
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

	// Marshal details to JSON
	var detailsJSON []byte
	var err error
	if log.Details != nil {
		detailsJSON, err = json.Marshal(log.Details)
		if err != nil {
			return fmt.Errorf("failed to marshal details: %w", err)
		}
	}

	query := `INSERT INTO audit_logs (
		id, timestamp, action, result, service_name, pod_name,
		details, error_msg, previous_status, new_status,
		consecutive_failures, failure_reason
	) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

	_, err = a.db.ExecContext(ctx, query,
		log.ID,
		log.Timestamp,
		log.Action,
		log.Result,
		log.ServiceName,
		log.PodName,
		detailsJSON,
		nullString(log.ErrorMsg),
		nullString(string(log.PreviousStatus)),
		nullString(string(log.NewStatus)),
		nullInt(log.ConsecutiveFailures),
		nullString(log.FailureReason),
	)

	if err != nil {
		return fmt.Errorf("failed to insert audit log: %w", err)
	}

	return nil
}

// GetAuditLogs retrieves audit logs based on filter criteria
func (a *AuditStore) GetAuditLogs(ctx context.Context, filter *models.AuditLogFilter) ([]models.AuditLog, int, error) {
	// Build WHERE clause
	whereClauses := []string{}
	args := []interface{}{}
	argCounter := 1

	if filter != nil {
		if filter.StartTime != nil {
			whereClauses = append(whereClauses, fmt.Sprintf("timestamp >= $%d", argCounter))
			args = append(args, *filter.StartTime)
			argCounter++
		}

		if filter.EndTime != nil {
			whereClauses = append(whereClauses, fmt.Sprintf("timestamp <= $%d", argCounter))
			args = append(args, *filter.EndTime)
			argCounter++
		}

		if len(filter.Actions) > 0 {
			placeholders := []string{}
			for _, action := range filter.Actions {
				placeholders = append(placeholders, fmt.Sprintf("$%d", argCounter))
				args = append(args, action)
				argCounter++
			}
			whereClauses = append(whereClauses, fmt.Sprintf("action IN (%s)", strings.Join(placeholders, ",")))
		}

		if len(filter.Results) > 0 {
			placeholders := []string{}
			for _, result := range filter.Results {
				placeholders = append(placeholders, fmt.Sprintf("$%d", argCounter))
				args = append(args, result)
				argCounter++
			}
			whereClauses = append(whereClauses, fmt.Sprintf("result IN (%s)", strings.Join(placeholders, ",")))
		}

		if filter.ServiceName != nil {
			whereClauses = append(whereClauses, fmt.Sprintf("service_name = $%d", argCounter))
			args = append(args, *filter.ServiceName)
			argCounter++
		}

		if filter.PodName != nil {
			whereClauses = append(whereClauses, fmt.Sprintf("pod_name = $%d", argCounter))
			args = append(args, *filter.PodName)
			argCounter++
		}
	}

	whereClause := ""
	if len(whereClauses) > 0 {
		whereClause = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM audit_logs %s", whereClause)
	var total int
	err := a.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count audit logs: %w", err)
	}

	// Set default pagination
	limit := 100
	offset := 0
	if filter != nil {
		if filter.Limit > 0 {
			limit = filter.Limit
		}
		if filter.Offset > 0 {
			offset = filter.Offset
		}
	}

	// Build query with pagination
	query := fmt.Sprintf(`SELECT
		id, timestamp, action, result, service_name, pod_name,
		details, error_msg, previous_status, new_status,
		consecutive_failures, failure_reason
		FROM audit_logs %s
		ORDER BY timestamp DESC
		LIMIT $%d OFFSET $%d`,
		whereClause, argCounter, argCounter+1)

	args = append(args, limit, offset)

	rows, err := a.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to query audit logs: %w", err)
	}
	defer rows.Close()

	logs := []models.AuditLog{}
	for rows.Next() {
		var log models.AuditLog
		var detailsJSON []byte
		var errorMsg, previousStatus, newStatus, failureReason sql.NullString
		var consecutiveFailures sql.NullInt64

		err := rows.Scan(
			&log.ID,
			&log.Timestamp,
			&log.Action,
			&log.Result,
			&log.ServiceName,
			&log.PodName,
			&detailsJSON,
			&errorMsg,
			&previousStatus,
			&newStatus,
			&consecutiveFailures,
			&failureReason,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan audit log: %w", err)
		}

		// Unmarshal details
		if len(detailsJSON) > 0 {
			if err := json.Unmarshal(detailsJSON, &log.Details); err != nil {
				return nil, 0, fmt.Errorf("failed to unmarshal details: %w", err)
			}
		}

		// Set nullable fields
		if errorMsg.Valid {
			log.ErrorMsg = errorMsg.String
		}
		if previousStatus.Valid {
			log.PreviousStatus = models.ServiceStatus(previousStatus.String)
		}
		if newStatus.Valid {
			log.NewStatus = models.ServiceStatus(newStatus.String)
		}
		if consecutiveFailures.Valid {
			log.ConsecutiveFailures = int(consecutiveFailures.Int64)
		}
		if failureReason.Valid {
			log.FailureReason = failureReason.String
		}

		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("failed to iterate audit logs: %w", err)
	}

	return logs, total, nil
}

// GetAuditLogByID retrieves a specific audit log by its ID
func (a *AuditStore) GetAuditLogByID(ctx context.Context, id string) (*models.AuditLog, error) {
	query := `SELECT
		id, timestamp, action, result, service_name, pod_name,
		details, error_msg, previous_status, new_status,
		consecutive_failures, failure_reason
		FROM audit_logs WHERE id = $1`

	var log models.AuditLog
	var detailsJSON []byte
	var errorMsg, previousStatus, newStatus, failureReason sql.NullString
	var consecutiveFailures sql.NullInt64

	err := a.db.QueryRowContext(ctx, query, id).Scan(
		&log.ID,
		&log.Timestamp,
		&log.Action,
		&log.Result,
		&log.ServiceName,
		&log.PodName,
		&detailsJSON,
		&errorMsg,
		&previousStatus,
		&newStatus,
		&consecutiveFailures,
		&failureReason,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("audit log not found: %s", id)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get audit log: %w", err)
	}

	// Unmarshal details
	if len(detailsJSON) > 0 {
		if err := json.Unmarshal(detailsJSON, &log.Details); err != nil {
			return nil, fmt.Errorf("failed to unmarshal details: %w", err)
		}
	}

	// Set nullable fields
	if errorMsg.Valid {
		log.ErrorMsg = errorMsg.String
	}
	if previousStatus.Valid {
		log.PreviousStatus = models.ServiceStatus(previousStatus.String)
	}
	if newStatus.Valid {
		log.NewStatus = models.ServiceStatus(newStatus.String)
	}
	if consecutiveFailures.Valid {
		log.ConsecutiveFailures = int(consecutiveFailures.Int64)
	}
	if failureReason.Valid {
		log.FailureReason = failureReason.String
	}

	return &log, nil
}

// DeleteOldAuditLogs deletes audit logs older than the specified retention period
func (a *AuditStore) DeleteOldAuditLogs(ctx context.Context, retentionDays int) (int, error) {
	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)

	query := `DELETE FROM audit_logs WHERE timestamp < $1`

	result, err := a.db.ExecContext(ctx, query, cutoffTime)
	if err != nil {
		return 0, fmt.Errorf("failed to delete old audit logs: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}

// Close closes the database connection
func (a *AuditStore) Close() error {
	return a.db.Close()
}

// Ping checks if the database is accessible
func (a *AuditStore) Ping(ctx context.Context) error {
	return a.db.PingContext(ctx)
}

// Helper functions for nullable fields
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: s, Valid: true}
}

func nullInt(i int) sql.NullInt64 {
	if i == 0 {
		return sql.NullInt64{Valid: false}
	}
	return sql.NullInt64{Int64: int64(i), Valid: true}
}
