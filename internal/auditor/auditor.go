package auditor

import (
	"context"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
)

// Auditor is responsible for logging governance events for audit purposes
type Auditor struct {
	store  storage.AuditStore
	logger logger.Logger
}

// NewAuditor creates a new auditor instance
func NewAuditor(store storage.AuditStore, log logger.Logger) *Auditor {
	return &Auditor{
		store:  store,
		logger: log,
	}
}

// LogRegister logs a service registration event
func (a *Auditor) LogRegister(ctx context.Context, serviceName, podName string, result models.AuditResult, details map[string]interface{}, errorMsg string) {
	log := &models.AuditLog{
		Action:      models.AuditActionRegister,
		Result:      result,
		ServiceName: serviceName,
		PodName:     podName,
		Details:     details,
		ErrorMsg:    errorMsg,
	}

	if err := a.store.SaveAuditLog(ctx, log); err != nil {
		a.logger.Errorw("Failed to save audit log for register event",
			"service_name", serviceName,
			"pod_name", podName,
			"error", err,
		)
	}
}

// LogUnregister logs a service unregistration event
func (a *Auditor) LogUnregister(ctx context.Context, serviceName, podName string, result models.AuditResult, details map[string]interface{}, errorMsg string) {
	log := &models.AuditLog{
		Action:      models.AuditActionUnregister,
		Result:      result,
		ServiceName: serviceName,
		PodName:     podName,
		Details:     details,
		ErrorMsg:    errorMsg,
	}

	if err := a.store.SaveAuditLog(ctx, log); err != nil {
		a.logger.Errorw("Failed to save audit log for unregister event",
			"service_name", serviceName,
			"pod_name", podName,
			"error", err,
		)
	}
}

// LogHeartbeat logs a heartbeat/health check event
func (a *Auditor) LogHeartbeat(ctx context.Context, serviceName, podName string, previousStatus, newStatus models.ServiceStatus, result models.AuditResult, details map[string]interface{}) {
	log := &models.AuditLog{
		Action:         models.AuditActionHeartbeat,
		Result:         result,
		ServiceName:    serviceName,
		PodName:        podName,
		PreviousStatus: previousStatus,
		NewStatus:      newStatus,
		Details:        details,
	}

	if err := a.store.SaveAuditLog(ctx, log); err != nil {
		a.logger.Errorw("Failed to save audit log for heartbeat event",
			"service_name", serviceName,
			"pod_name", podName,
			"error", err,
		)
	}
}

// LogHealthStatusChange logs a health status change event
func (a *Auditor) LogHealthStatusChange(ctx context.Context, serviceName, podName string, previousStatus, newStatus models.ServiceStatus, details map[string]interface{}) {
	log := &models.AuditLog{
		Action:         models.AuditActionHealthStatusChange,
		Result:         models.AuditResultSuccess,
		ServiceName:    serviceName,
		PodName:        podName,
		PreviousStatus: previousStatus,
		NewStatus:      newStatus,
		Details:        details,
	}

	if err := a.store.SaveAuditLog(ctx, log); err != nil {
		a.logger.Errorw("Failed to save audit log for health status change event",
			"service_name", serviceName,
			"pod_name", podName,
			"error", err,
		)
	}
}

// LogAutoCleanup logs an auto-cleanup event (service removed due to health check failures)
func (a *Auditor) LogAutoCleanup(ctx context.Context, serviceName, podName string, consecutiveFailures int, failureReason string, details map[string]interface{}) {
	log := &models.AuditLog{
		Action:              models.AuditActionAutoCleanup,
		Result:              models.AuditResultSuccess,
		ServiceName:         serviceName,
		PodName:             podName,
		ConsecutiveFailures: consecutiveFailures,
		FailureReason:       failureReason,
		Details:             details,
	}

	if err := a.store.SaveAuditLog(ctx, log); err != nil {
		a.logger.Errorw("Failed to save audit log for auto-cleanup event",
			"service_name", serviceName,
			"pod_name", podName,
			"error", err,
		)
	}
}

// LogReconcile logs a reconciliation event
func (a *Auditor) LogReconcile(ctx context.Context, result models.AuditResult, details map[string]interface{}) {
	log := &models.AuditLog{
		Action:      models.AuditActionReconcile,
		Result:      result,
		ServiceName: "system",
		PodName:     "reconcile",
		Details:     details,
	}

	if err := a.store.SaveAuditLog(ctx, log); err != nil {
		a.logger.Errorw("Failed to save audit log for reconcile event",
			"error", err,
		)
	}
}

// GetAuditLogs retrieves audit logs based on filter criteria
func (a *Auditor) GetAuditLogs(ctx context.Context, filter *models.AuditLogFilter) ([]models.AuditLog, int, error) {
	return a.store.GetAuditLogs(ctx, filter)
}

// GetAuditLogByID retrieves a specific audit log by its ID
func (a *Auditor) GetAuditLogByID(ctx context.Context, id string) (*models.AuditLog, error) {
	return a.store.GetAuditLogByID(ctx, id)
}

// DeleteOldAuditLogs deletes audit logs older than the specified retention period
func (a *Auditor) DeleteOldAuditLogs(ctx context.Context, retentionDays int) (int, error) {
	return a.store.DeleteOldAuditLogs(ctx, retentionDays)
}

// Close closes the auditor and releases resources
func (a *Auditor) Close() error {
	return a.store.Close()
}
