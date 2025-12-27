package scheduler

import (
	"time"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/events"
	"github.com/chronnie/governance/internal/registry"
	"github.com/chronnie/governance/pkg/logger"
)

// HealthCheckScheduler periodically schedules health check events for all services
type HealthCheckScheduler struct {
	registry   *registry.Registry
	eventQueue eventqueue.IEventQueue
	interval   time.Duration
	stopChan   chan struct{}
	logger     logger.Logger
}

// NewHealthCheckScheduler creates a new health check scheduler
func NewHealthCheckScheduler(reg *registry.Registry, eventQueue eventqueue.IEventQueue, interval time.Duration, log logger.Logger) *HealthCheckScheduler {
	return &HealthCheckScheduler{
		registry:   reg,
		eventQueue: eventQueue,
		interval:   interval,
		stopChan:   make(chan struct{}),
		logger:     log,
	}
}

// Start begins the health check scheduling
func (s *HealthCheckScheduler) Start() {
	s.logger.Infow("HealthCheckScheduler: Starting health check scheduler",
		"interval", s.interval,
	)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.logger.Debugw("HealthCheckScheduler: Ticker fired, scheduling health checks")
			s.scheduleHealthChecks()
		case <-s.stopChan:
			s.logger.Infow("HealthCheckScheduler: Stopping health check scheduler")
			return
		}
	}
}

// Stop stops the health check scheduler
func (s *HealthCheckScheduler) Stop() {
	s.logger.Debugw("HealthCheckScheduler: Stop signal sent")
	close(s.stopChan)
}

// scheduleHealthChecks creates health check events for all registered services
func (s *HealthCheckScheduler) scheduleHealthChecks() {
	services := s.registry.GetAllServices()

	s.logger.Debugw("HealthCheckScheduler: Scheduling health checks for all services",
		"service_count", len(services),
	)

	for _, service := range services {
		s.logger.Debugw("HealthCheckScheduler: Enqueuing health check event",
			"service_key", service.GetKey(),
			"service_name", service.ServiceName,
			"pod_name", service.PodName,
		)

		// Create context with event data
		ctx := events.NewHealthCheckContext(service.GetKey())

		// Create event (without deadline for health checks)
		event := eventqueue.NewEvent(string(events.EventHealthCheck), ctx)

		// Enqueue event
		s.eventQueue.Enqueue(event)
	}

	s.logger.Infow("HealthCheckScheduler: Scheduled health checks",
		"events_enqueued", len(services),
	)
}

// ReconcileScheduler periodically schedules reconcile events
type ReconcileScheduler struct {
	eventQueue eventqueue.IEventQueue
	interval   time.Duration
	stopChan   chan struct{}
	logger     logger.Logger
}

// NewReconcileScheduler creates a new reconcile scheduler
func NewReconcileScheduler(eventQueue eventqueue.IEventQueue, interval time.Duration, log logger.Logger) *ReconcileScheduler {
	return &ReconcileScheduler{
		eventQueue: eventQueue,
		interval:   interval,
		stopChan:   make(chan struct{}),
		logger:     log,
	}
}

// Start begins the reconcile scheduling
func (s *ReconcileScheduler) Start() {
	s.logger.Infow("ReconcileScheduler: Starting reconcile scheduler",
		"interval", s.interval,
	)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.logger.Debugw("ReconcileScheduler: Ticker fired, scheduling reconcile")
			s.scheduleReconcile()
		case <-s.stopChan:
			s.logger.Infow("ReconcileScheduler: Stopping reconcile scheduler")
			return
		}
	}
}

// Stop stops the reconcile scheduler
func (s *ReconcileScheduler) Stop() {
	s.logger.Debugw("ReconcileScheduler: Stop signal sent")
	close(s.stopChan)
}

// scheduleReconcile creates a reconcile event
func (s *ReconcileScheduler) scheduleReconcile() {
	s.logger.Infow("ReconcileScheduler: Enqueuing reconcile event")

	// Create context with event data
	ctx := events.NewReconcileContext()

	// Create event (without deadline for reconcile)
	event := eventqueue.NewEvent(string(events.EventReconcile), ctx)

	// Enqueue event
	s.eventQueue.Enqueue(event)

	s.logger.Debugw("ReconcileScheduler: Reconcile event enqueued")
}
