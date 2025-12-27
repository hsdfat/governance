package models

import "time"

// AuditAction represents the type of action performed
type AuditAction string

const (
	AuditActionRegister          AuditAction = "register"
	AuditActionUnregister        AuditAction = "unregister"
	AuditActionHeartbeat         AuditAction = "heartbeat"
	AuditActionAutoCleanup       AuditAction = "auto_cleanup"
	AuditActionHealthStatusChange AuditAction = "health_status_change"
	AuditActionReconcile         AuditAction = "reconcile"
)

// AuditResult represents the outcome of an action
type AuditResult string

const (
	AuditResultSuccess AuditResult = "success"
	AuditResultFailure AuditResult = "failure"
	AuditResultPartial AuditResult = "partial"
)

// AuditLog represents a single audit log entry for governance events
type AuditLog struct {
	ID          string      `json:"id"`           // Unique identifier for the audit log
	Timestamp   time.Time   `json:"timestamp"`    // When the action occurred
	Action      AuditAction `json:"action"`       // Type of action performed
	Result      AuditResult `json:"result"`       // Outcome of the action
	ServiceName string      `json:"service_name"` // Service involved in the action
	PodName     string      `json:"pod_name"`     // Pod involved in the action

	// Additional context
	Details     map[string]interface{} `json:"details,omitempty"`     // Action-specific details
	ErrorMsg    string                 `json:"error_msg,omitempty"`   // Error message if result is failure

	// Health check specific fields
	PreviousStatus ServiceStatus `json:"previous_status,omitempty"` // Previous health status
	NewStatus      ServiceStatus `json:"new_status,omitempty"`      // New health status

	// Auto-cleanup specific fields
	ConsecutiveFailures int    `json:"consecutive_failures,omitempty"` // Number of consecutive failures before cleanup
	FailureReason       string `json:"failure_reason,omitempty"`       // Reason for auto-cleanup
}

// AuditLogFilter represents filter criteria for querying audit logs
type AuditLogFilter struct {
	// Time range filters
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`

	// Action filters
	Actions []AuditAction `json:"actions,omitempty"`

	// Result filters
	Results []AuditResult `json:"results,omitempty"`

	// Service filters
	ServiceName *string `json:"service_name,omitempty"`
	PodName     *string `json:"pod_name,omitempty"`

	// Pagination
	Limit  int `json:"limit,omitempty"`  // Maximum number of results (default: 100)
	Offset int `json:"offset,omitempty"` // Number of results to skip
}

// AuditLogResponse represents the API response for audit log queries
type AuditLogResponse struct {
	Total int         `json:"total"` // Total number of matching records
	Logs  []AuditLog  `json:"logs"`  // Audit log entries
}
