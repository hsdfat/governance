package storage

import (
	"context"

	"github.com/chronnie/governance/models"
)

// AuditStore defines the interface for persisting audit log data.
// Implementations can use different storage backends (memory, MySQL, PostgreSQL, MongoDB, etc.)
type AuditStore interface {
	// SaveAuditLog stores a new audit log entry
	SaveAuditLog(ctx context.Context, log *models.AuditLog) error

	// GetAuditLogs retrieves audit logs based on filter criteria
	GetAuditLogs(ctx context.Context, filter *models.AuditLogFilter) ([]models.AuditLog, int, error)

	// GetAuditLogByID retrieves a specific audit log by its ID
	GetAuditLogByID(ctx context.Context, id string) (*models.AuditLog, error)

	// DeleteOldAuditLogs deletes audit logs older than the specified retention period
	DeleteOldAuditLogs(ctx context.Context, retentionDays int) (int, error)

	// Close closes the storage connection and cleans up resources
	Close() error

	// Ping checks if the storage backend is accessible
	Ping(ctx context.Context) error
}
