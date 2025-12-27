package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/google/uuid"
)

// AuditStore is an in-memory implementation of the audit log storage
type AuditStore struct {
	mu   sync.RWMutex
	logs map[string]*models.AuditLog // key: audit log ID
}

// NewAuditStore creates a new in-memory audit store
func NewAuditStore() *AuditStore {
	return &AuditStore{
		logs: make(map[string]*models.AuditLog),
	}
}

// SaveAuditLog stores a new audit log entry
func (s *AuditStore) SaveAuditLog(ctx context.Context, log *models.AuditLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate ID if not provided
	if log.ID == "" {
		log.ID = uuid.New().String()
	}

	// Set timestamp if not provided
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now()
	}

	// Store a copy to prevent external modifications
	logCopy := *log
	s.logs[log.ID] = &logCopy

	return nil
}

// GetAuditLogs retrieves audit logs based on filter criteria
func (s *AuditStore) GetAuditLogs(ctx context.Context, filter *models.AuditLogFilter) ([]models.AuditLog, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Collect all logs into a slice
	allLogs := make([]*models.AuditLog, 0, len(s.logs))
	for _, log := range s.logs {
		allLogs = append(allLogs, log)
	}

	// Sort by timestamp descending (newest first)
	sort.Slice(allLogs, func(i, j int) bool {
		return allLogs[i].Timestamp.After(allLogs[j].Timestamp)
	})

	// Apply filters
	filtered := make([]*models.AuditLog, 0)
	for _, log := range allLogs {
		if s.matchesFilter(log, filter) {
			filtered = append(filtered, log)
		}
	}

	total := len(filtered)

	// Apply pagination
	limit := filter.Limit
	if limit <= 0 {
		limit = 100 // default limit
	}

	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	// Calculate pagination bounds
	start := offset
	if start > total {
		start = total
	}

	end := start + limit
	if end > total {
		end = total
	}

	// Extract the page
	page := filtered[start:end]

	// Convert to value slice
	result := make([]models.AuditLog, len(page))
	for i, log := range page {
		result[i] = *log
	}

	return result, total, nil
}

// matchesFilter checks if a log entry matches the filter criteria
func (s *AuditStore) matchesFilter(log *models.AuditLog, filter *models.AuditLogFilter) bool {
	if filter == nil {
		return true
	}

	// Time range filter
	if filter.StartTime != nil && log.Timestamp.Before(*filter.StartTime) {
		return false
	}
	if filter.EndTime != nil && log.Timestamp.After(*filter.EndTime) {
		return false
	}

	// Action filter
	if len(filter.Actions) > 0 {
		found := false
		for _, action := range filter.Actions {
			if log.Action == action {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Result filter
	if len(filter.Results) > 0 {
		found := false
		for _, result := range filter.Results {
			if log.Result == result {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Service name filter
	if filter.ServiceName != nil && log.ServiceName != *filter.ServiceName {
		return false
	}

	// Pod name filter
	if filter.PodName != nil && log.PodName != *filter.PodName {
		return false
	}

	return true
}

// GetAuditLogByID retrieves a specific audit log by its ID
func (s *AuditStore) GetAuditLogByID(ctx context.Context, id string) (*models.AuditLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	log, exists := s.logs[id]
	if !exists {
		return nil, fmt.Errorf("audit log not found: %s", id)
	}

	// Return a copy
	logCopy := *log
	return &logCopy, nil
}

// DeleteOldAuditLogs deletes audit logs older than the specified retention period
func (s *AuditStore) DeleteOldAuditLogs(ctx context.Context, retentionDays int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoffTime := time.Now().AddDate(0, 0, -retentionDays)
	deletedCount := 0

	for id, log := range s.logs {
		if log.Timestamp.Before(cutoffTime) {
			delete(s.logs, id)
			deletedCount++
		}
	}

	return deletedCount, nil
}

// Close closes the storage connection
func (s *AuditStore) Close() error {
	// Nothing to close for in-memory store
	return nil
}

// Ping checks if the storage is accessible
func (s *AuditStore) Ping(ctx context.Context) error {
	// In-memory store is always available
	return nil
}
