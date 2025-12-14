package manager

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/internal/api"
	"github.com/chronnie/governance/internal/notifier"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/internal/scheduler"
	"github.com/chronnie/governance/internal/worker"
	"github.com/chronnie/governance/models"
)

// Manager is the main governance manager component
type Manager struct {
	config *models.ManagerConfig

	// Core components
	registry      *registry.Registry
	eventQueue    eventqueue.IEventQueue
	notifier      *notifier.Notifier
	healthChecker *notifier.HealthChecker
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

// NewManager creates a new governance manager
func NewManager(config *models.ManagerConfig) *Manager {
	if config == nil {
		config = models.DefaultConfig()
	}

	// Create registry
	reg := registry.NewRegistry()

	// Create event queue with Sequential mode for FIFO processing
	queueConfig := eventqueue.EventQueueConfig{
		BufferSize:     config.EventQueueSize,
		ProcessingMode: eventqueue.Sequential, // Sequential for FIFO event processing
	}
	eventQueue := eventqueue.NewEventQueue(queueConfig)

	// Create notifier
	notif := notifier.NewNotifier(config.NotificationTimeout)

	// Create health checker
	healthCheck := notifier.NewHealthChecker(config.HealthCheckTimeout, config.HealthCheckRetry)

	// Create event worker and register handlers
	eventWorker := worker.NewEventWorker(reg, notif, healthCheck)
	eventWorker.RegisterHandlers(eventQueue)

	// Create schedulers
	healthCheckScheduler := scheduler.NewHealthCheckScheduler(reg, eventQueue, config.HealthCheckInterval)
	reconcileScheduler := scheduler.NewReconcileScheduler(eventQueue, config.NotificationInterval)

	// Create HTTP handler
	handler := api.NewHandler(reg, eventQueue)

	// Setup HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/register", handler.RegisterHandler)
	mux.HandleFunc("/unregister", handler.UnregisterHandler)
	mux.HandleFunc("/services", handler.ServicesHandler)
	mux.HandleFunc("/health", handler.HealthHandler)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", config.ServerPort),
		Handler: mux,
	}

	// Create context for queue
	queueCtx, queueCancel := context.WithCancel(context.Background())

	return &Manager{
		config:               config,
		registry:             reg,
		eventQueue:           eventQueue,
		notifier:             notif,
		healthChecker:        healthCheck,
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
	log.Println("[Manager] Starting governance manager...")

	// Start event queue
	go func() {
		if err := m.eventQueue.Start(m.queueContext); err != nil {
			log.Printf("[Manager] Event queue error: %v", err)
		}
	}()

	// Start schedulers
	go m.healthCheckScheduler.Start()
	go m.reconcileScheduler.Start()

	// Start HTTP server
	go func() {
		log.Printf("[Manager] HTTP server starting on port %d", m.config.ServerPort)
		if err := m.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[Manager] HTTP server error: %v", err)
		}
	}()

	log.Println("[Manager] Governance manager started successfully")
	log.Printf("[Manager] Configuration: health_check_interval=%v, notification_interval=%v",
		m.config.HealthCheckInterval, m.config.NotificationInterval)

	return nil
}

// Stop gracefully stops the governance manager
func (m *Manager) Stop() error {
	log.Println("[Manager] Stopping governance manager...")

	// Stop schedulers
	m.healthCheckScheduler.Stop()
	m.reconcileScheduler.Stop()

	// Stop HTTP server
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := m.httpServer.Shutdown(ctx); err != nil {
		log.Printf("[Manager] HTTP server shutdown error: %v", err)
	}

	// Stop event queue
	if err := m.eventQueue.Stop(); err != nil {
		log.Printf("[Manager] Event queue stop error: %v", err)
	}
	m.queueCancel()

	// Close stop channel
	close(m.stopChan)

	log.Println("[Manager] Governance manager stopped")
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
