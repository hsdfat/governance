package auditor

import (
	"context"
	"testing"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage/memory"
)

func TestAuditor_LogRegister(t *testing.T) {
	store := memory.NewAuditStore()
	log := logger.Log
	aud := NewAuditor(store, log)

	ctx := context.Background()

	// Log a register event
	aud.LogRegister(ctx, "test-service", "pod-1", models.AuditResultSuccess, map[string]interface{}{
		"health_check_url": "http://localhost:8080/health",
		"providers_count":  2,
	}, "")

	// Retrieve logs
	logs, total, err := aud.GetAuditLogs(ctx, &models.AuditLogFilter{})
	if err != nil {
		t.Fatalf("Failed to get audit logs: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 audit log, got %d", total)
	}

	if len(logs) != 1 {
		t.Errorf("Expected 1 audit log in result, got %d", len(logs))
	}

	auditLog := logs[0]
	if auditLog.Action != models.AuditActionRegister {
		t.Errorf("Expected action %s, got %s", models.AuditActionRegister, auditLog.Action)
	}

	if auditLog.Result != models.AuditResultSuccess {
		t.Errorf("Expected result %s, got %s", models.AuditResultSuccess, auditLog.Result)
	}

	if auditLog.ServiceName != "test-service" {
		t.Errorf("Expected service_name 'test-service', got %s", auditLog.ServiceName)
	}

	if auditLog.PodName != "pod-1" {
		t.Errorf("Expected pod_name 'pod-1', got %s", auditLog.PodName)
	}
}

func TestAuditor_FilterByAction(t *testing.T) {
	store := memory.NewAuditStore()
	log := logger.Log
	aud := NewAuditor(store, log)

	ctx := context.Background()

	// Log multiple events
	aud.LogRegister(ctx, "service-1", "pod-1", models.AuditResultSuccess, nil, "")
	aud.LogUnregister(ctx, "service-1", "pod-1", models.AuditResultSuccess, nil, "")
	aud.LogHeartbeat(ctx, "service-2", "pod-1", models.StatusUnknown, models.StatusHealthy, models.AuditResultSuccess, nil)

	// Filter by register action
	logs, total, err := aud.GetAuditLogs(ctx, &models.AuditLogFilter{
		Actions: []models.AuditAction{models.AuditActionRegister},
	})
	if err != nil {
		t.Fatalf("Failed to get audit logs: %v", err)
	}

	if total != 1 {
		t.Errorf("Expected 1 audit log for register action, got %d", total)
	}

	if len(logs) != 1 {
		t.Errorf("Expected 1 audit log in result, got %d", len(logs))
	}

	if logs[0].Action != models.AuditActionRegister {
		t.Errorf("Expected action %s, got %s", models.AuditActionRegister, logs[0].Action)
	}
}

func TestAuditor_Pagination(t *testing.T) {
	store := memory.NewAuditStore()
	log := logger.Log
	aud := NewAuditor(store, log)

	ctx := context.Background()

	// Log 10 events
	for i := 0; i < 10; i++ {
		aud.LogRegister(ctx, "service-1", "pod-1", models.AuditResultSuccess, nil, "")
	}

	// Get first page (limit 5)
	logs, total, err := aud.GetAuditLogs(ctx, &models.AuditLogFilter{
		Limit: 5,
	})
	if err != nil {
		t.Fatalf("Failed to get audit logs: %v", err)
	}

	if total != 10 {
		t.Errorf("Expected total 10, got %d", total)
	}

	if len(logs) != 5 {
		t.Errorf("Expected 5 logs in first page, got %d", len(logs))
	}
}
