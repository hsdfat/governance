package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chronnie/governance/internal/auditor"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
)

// AuditHandler handles HTTP requests for audit logs
type AuditHandler struct {
	auditor *auditor.Auditor
	logger  logger.Logger
}

// NewAuditHandler creates a new audit handler
func NewAuditHandler(aud *auditor.Auditor, log logger.Logger) *AuditHandler {
	return &AuditHandler{
		auditor: aud,
		logger:  log,
	}
}

// GetAuditLogsHandler handles GET /audit/logs requests
func (h *AuditHandler) GetAuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Infow("API: Received get audit logs request",
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	if r.Method != http.MethodGet {
		h.logger.Warnw("API: Invalid method for audit logs endpoint",
			"method", r.Method,
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse query parameters
	query := r.URL.Query()
	filter := &models.AuditLogFilter{}

	// Time range filters
	if startTimeStr := query.Get("start_time"); startTimeStr != "" {
		startTime, err := time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			h.logger.Errorw("API: Invalid start_time format",
				"start_time", startTimeStr,
				"error", err,
			)
			http.Error(w, "Invalid start_time format, use RFC3339", http.StatusBadRequest)
			return
		}
		filter.StartTime = &startTime
	}

	if endTimeStr := query.Get("end_time"); endTimeStr != "" {
		endTime, err := time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			h.logger.Errorw("API: Invalid end_time format",
				"end_time", endTimeStr,
				"error", err,
			)
			http.Error(w, "Invalid end_time format, use RFC3339", http.StatusBadRequest)
			return
		}
		filter.EndTime = &endTime
	}

	// Action filters
	if actionsStr := query.Get("actions"); actionsStr != "" {
		actionStrs := strings.Split(actionsStr, ",")
		actions := make([]models.AuditAction, 0, len(actionStrs))
		for _, actionStr := range actionStrs {
			actions = append(actions, models.AuditAction(strings.TrimSpace(actionStr)))
		}
		filter.Actions = actions
	}

	// Result filters
	if resultsStr := query.Get("results"); resultsStr != "" {
		resultStrs := strings.Split(resultsStr, ",")
		results := make([]models.AuditResult, 0, len(resultStrs))
		for _, resultStr := range resultStrs {
			results = append(results, models.AuditResult(strings.TrimSpace(resultStr)))
		}
		filter.Results = results
	}

	// Service name filter
	if serviceName := query.Get("service_name"); serviceName != "" {
		filter.ServiceName = &serviceName
	}

	// Pod name filter
	if podName := query.Get("pod_name"); podName != "" {
		filter.PodName = &podName
	}

	// Pagination
	if limitStr := query.Get("limit"); limitStr != "" {
		limit, err := strconv.Atoi(limitStr)
		if err != nil || limit <= 0 {
			h.logger.Errorw("API: Invalid limit value",
				"limit", limitStr,
				"error", err,
			)
			http.Error(w, "Invalid limit value", http.StatusBadRequest)
			return
		}
		filter.Limit = limit
	}

	if offsetStr := query.Get("offset"); offsetStr != "" {
		offset, err := strconv.Atoi(offsetStr)
		if err != nil || offset < 0 {
			h.logger.Errorw("API: Invalid offset value",
				"offset", offsetStr,
				"error", err,
			)
			http.Error(w, "Invalid offset value", http.StatusBadRequest)
			return
		}
		filter.Offset = offset
	}

	// Get audit logs
	logs, total, err := h.auditor.GetAuditLogs(r.Context(), filter)
	if err != nil {
		h.logger.Errorw("API: Failed to get audit logs",
			"error", err,
		)
		http.Error(w, "Failed to get audit logs", http.StatusInternalServerError)
		return
	}

	// Build response
	response := models.AuditLogResponse{
		Total: total,
		Logs:  logs,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		h.logger.Errorw("API: Failed to encode audit logs response",
			"error", err,
		)
	}

	h.logger.Infow("API: Audit logs retrieved successfully",
		"total", total,
		"returned", len(logs),
	)
}

// GetAuditLogByIDHandler handles GET /audit/logs/:id requests
func (h *AuditHandler) GetAuditLogByIDHandler(w http.ResponseWriter, r *http.Request) {
	h.logger.Infow("API: Received get audit log by ID request",
		"method", r.Method,
		"remote_addr", r.RemoteAddr,
	)

	if r.Method != http.MethodGet {
		h.logger.Warnw("API: Invalid method for audit log by ID endpoint",
			"method", r.Method,
		)
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extract ID from URL path
	// Assuming URL pattern: /audit/logs/{id}
	id := r.URL.Query().Get("id")
	if id == "" {
		h.logger.Warnw("API: Missing audit log ID")
		http.Error(w, "Missing audit log ID", http.StatusBadRequest)
		return
	}

	// Get audit log
	log, err := h.auditor.GetAuditLogByID(r.Context(), id)
	if err != nil {
		h.logger.Errorw("API: Failed to get audit log by ID",
			"id", id,
			"error", err,
		)
		http.Error(w, "Audit log not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(log); err != nil {
		h.logger.Errorw("API: Failed to encode audit log response",
			"error", err,
		)
	}

	h.logger.Infow("API: Audit log retrieved successfully",
		"id", id,
	)
}
