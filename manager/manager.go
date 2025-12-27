package manager

import (
	"context"
	"fmt"
	"net/http"
	"time"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/internal/api"
	"github.com/chronnie/governance/internal/auditor"
	"github.com/chronnie/governance/internal/notifier"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/internal/scheduler"
	"github.com/chronnie/governance/internal/worker"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
	"github.com/chronnie/governance/storage"
	"github.com/chronnie/governance/storage/memory"
)

// Manager is the main governance manager component
type Manager struct {
	config *models.ManagerConfig
	logger logger.Logger

	// Core components
	dualStore     *storage.DualStore // Always uses in-memory cache + optional database
	registry      *registry.Registry
	eventQueue    eventqueue.IEventQueue
	notifier      *notifier.Notifier
	healthChecker *notifier.HealthChecker
	auditor       *auditor.Auditor // Audit logger
	eventWorker   *worker.EventWorker
	queueContext  context.Context
	queueCancel   context.CancelFunc

	// Schedulers
	healthCheckScheduler *scheduler.HealthCheckScheduler
	reconcileScheduler   *scheduler.ReconcileScheduler

	// HTTP server
	httpServer *http.Server

	// Lifecycle
	stopChan chan struct{}
}

// NewManager creates a new governance manager with in-memory cache only (no database persistence)
func NewManager(config *models.ManagerConfig) *Manager {
	return NewManagerWithDatabase(config, nil)
}

// NewManagerWithDatabase creates a new governance manager with optional database persistence.
// The manager always uses in-memory cache for performance.
// If db is not nil, all changes are also persisted to the database asynchronously.
func NewManagerWithDatabase(config *models.ManagerConfig, db storage.DatabaseStore) *Manager {
	return NewManagerWithDatabaseAndAudit(config, db, nil)
}

// NewManagerWithDatabaseAndAudit creates a new governance manager with optional database persistence and audit store.
// The manager always uses in-memory cache for performance.
// If db is not nil, all changes are also persisted to the database asynchronously.
// If auditStore is not nil, all governance events are logged to the audit store.
// If auditStore is nil, audit logs are stored in-memory only.
func NewManagerWithDatabaseAndAudit(config *models.ManagerConfig, db storage.DatabaseStore, auditStore storage.AuditStore) *Manager {
	if config == nil {
		config = models.DefaultConfig()
	}

	// Create logger
	log := logger.Log

	// Create dual-layer storage (always has cache, database is optional)
	dualStore := storage.NewDualStore(db)

	// Create registry with dual store
	reg := registry.NewRegistry(dualStore, log)

	// Create event queue with Sequential mode for FIFO processing
	queueConfig := eventqueue.EventQueueConfig{
		BufferSize:     config.EventQueueSize,
		ProcessingMode: eventqueue.Sequential, // Sequential for FIFO event processing
	}
	eventQueue := eventqueue.NewEventQueue(queueConfig)

	// Create notifier
	notif := notifier.NewNotifier(config.NotificationTimeout, log)

	// Create health checker
	healthCheck := notifier.NewHealthChecker(config.HealthCheckTimeout, config.HealthCheckRetry, log)

	// Create audit store (use in-memory if not provided)
	if auditStore == nil {
		auditStore = memory.NewAuditStore()
	}

	// Create auditor
	aud := auditor.NewAuditor(auditStore, log)

	// Create event worker and register handlers
	eventWorker := worker.NewEventWorker(reg, notif, healthCheck, dualStore, aud, config, log)
	eventWorker.RegisterHandlers(eventQueue)

	// Create schedulers
	healthCheckScheduler := scheduler.NewHealthCheckScheduler(reg, eventQueue, config.HealthCheckInterval, log)
	reconcileScheduler := scheduler.NewReconcileScheduler(eventQueue, config.NotificationInterval, log)

	// Create HTTP handler
	handler := api.NewHandler(reg, eventQueue, log)

	// Create audit HTTP handler
	auditHandler := api.NewAuditHandler(aud, log)

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/register", handler.RegisterHandler)
	mux.HandleFunc("/unregister", handler.UnregisterHandler)
	mux.HandleFunc("/heartbeat", handler.HeartbeatHandler)
	mux.HandleFunc("/services", handler.ServicesHandler)
	mux.HandleFunc("/health", handler.HealthHandler)

	// Audit endpoints
	mux.HandleFunc("/audit/logs", auditHandler.GetAuditLogsHandler)
	mux.HandleFunc("/audit/logs/by-id", auditHandler.GetAuditLogByIDHandler)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.ServerPort),
		Handler: mux,
	}

	// Create context for queue
	queueCtx, queueCancel := context.WithCancel(context.Background())

	return &Manager{
		config:               config,
		logger:               log,
		dualStore:            dualStore,
		registry:             reg,
		eventQueue:           eventQueue,
		notifier:             notif,
		healthChecker:        healthCheck,
		auditor:              aud,
		eventWorker:          eventWorker,
		healthCheckScheduler: healthCheckScheduler,
		reconcileScheduler:   reconcileScheduler,
		httpServer:           httpServer,
		stopChan:             make(chan struct{}),
		queueContext:         queueCtx,
		queueCancel:          queueCancel,
	}
}

// Start starts the governance manager
func (m *Manager) Start() error {
	m.logger.Info("Starting governance manager")

	// Start event queue
	go func() {
		if err := m.eventQueue.Start(m.queueContext); err != nil {
			m.logger.Errorw("Event queue error", "error", err)
		}
	}()

	// Start schedulers
	go m.healthCheckScheduler.Start()
	go m.reconcileScheduler.Start()

	// Start HTTP server
	go func() {
		m.logger.Infow("HTTP server starting", "port", m.config.ServerPort)
		if err := m.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			m.logger.Errorw("HTTP server error", "error", err)
		}
	}()

	m.logger.Infow("Governance manager started successfully",
		"health_check_interval", m.config.HealthCheckInterval,
		"notification_interval", m.config.NotificationInterval,
	)

	return nil
}

// Stop gracefully stops the governance manager
func (m *Manager) Stop() error {
	m.logger.Info("Stopping governance manager")

	// Stop schedulers
	m.healthCheckScheduler.Stop()
	m.reconcileScheduler.Stop()

	// Stop HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.httpServer.Shutdown(ctx); err != nil {
		m.logger.Errorw("HTTP server shutdown error", "error", err)
	}

	// Stop event queue
	if err := m.eventQueue.Stop(); err != nil {
		m.logger.Errorw("Event queue stop error", "error", err)
	}
	m.queueCancel()

	// Close storage connection (database if enabled)
	if err := m.dualStore.Close(); err != nil {
		m.logger.Errorw("Storage close error", "error", err)
	}

	// Close auditor
	if err := m.auditor.Close(); err != nil {
		m.logger.Errorw("Auditor close error", "error", err)
	}

	// Close stop channel
	close(m.stopChan)

	m.logger.Info("Governance manager stopped")
	logger.Sync() // Flush any buffered logs
	return nil
}

// Wait blocks until the manager is stopped
func (m *Manager) Wait() {
	<-m.stopChan
}

// GetRegistry returns the registry (for testing/debugging)
func (m *Manager) GetRegistry() *registry.Registry {
	return m.registry
}

// GetConfig returns the manager configuration
func (m *Manager) GetConfig() *models.ManagerConfig {
	return m.config
}

// GetServicePods returns all pods for a given service group
func (m *Manager) GetServicePods(serviceName string) []*models.ServiceInfo {
	return m.registry.GetByServiceName(serviceName)
}

// GetAllServicePods returns a map of service names to their pods
func (m *Manager) GetAllServicePods() map[string][]*models.ServiceInfo {
	allServices := m.registry.GetAllServices()
	result := make(map[string][]*models.ServiceInfo)

	for _, service := range allServices {
		result[service.ServiceName] = append(result[service.ServiceName], service)
	}

	return result
}
