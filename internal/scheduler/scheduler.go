package scheduler

import (
	"log"
	"time"

	eventqueue "github.com/chronnie/go-event-queue"
	"github.com/chronnie/governance/events"
	"github.com/chronnie/governance/internal/registry"
)

// HealthCheckScheduler periodically schedules health check events for all services
type HealthCheckScheduler struct {
	registry   *registry.Registry
	eventQueue eventqueue.IEventQueue
	interval   time.Duration
	stopChan   chan struct{}
}

// NewHealthCheckScheduler creates a new health check scheduler
func NewHealthCheckScheduler(reg *registry.Registry, eventQueue eventqueue.IEventQueue, interval time.Duration) *HealthCheckScheduler {
	return &HealthCheckScheduler{
		registry:   reg,
		eventQueue: eventQueue,
		interval:   interval,
		stopChan:   make(chan struct{}),
	}
}

// Start begins the health check scheduling
func (s *HealthCheckScheduler) Start() {
	log.Printf("[HealthCheckScheduler] Starting with interval: %v", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.scheduleHealthChecks()
		case <-s.stopChan:
			log.Println("[HealthCheckScheduler] Stopped")
			return
		}
	}
}

// Stop stops the health check scheduler
func (s *HealthCheckScheduler) Stop() {
	close(s.stopChan)
}

// scheduleHealthChecks creates health check events for all registered services
func (s *HealthCheckScheduler) scheduleHealthChecks() {
	services := s.registry.GetAllServices()
	log.Printf("[HealthCheckScheduler] Scheduling health checks for %d services", len(services))

	for _, service := range services {
		// Create context with event data
		ctx := events.NewHealthCheckContext(service.GetKey())

		// Create event (without deadline for health checks)
		event := eventqueue.NewEvent(string(events.EventHealthCheck), ctx)

		// Enqueue event
		err := s.eventQueue.Enqueue(event)
		if err != nil {
			log.Printf("[HealthCheckScheduler] Failed to enqueue health check event for %s: %v",
				service.GetKey(), err)
		}
	}
}

// ReconcileScheduler periodically schedules reconcile events
type ReconcileScheduler struct {
	eventQueue eventqueue.IEventQueue
	interval   time.Duration
	stopChan   chan struct{}
}

// NewReconcileScheduler creates a new reconcile scheduler
func NewReconcileScheduler(eventQueue eventqueue.IEventQueue, interval time.Duration) *ReconcileScheduler {
	return &ReconcileScheduler{
		eventQueue: eventQueue,
		interval:   interval,
		stopChan:   make(chan struct{}),
	}
}

// Start begins the reconcile scheduling
func (s *ReconcileScheduler) Start() {
	log.Printf("[ReconcileScheduler] Starting with interval: %v", s.interval)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.scheduleReconcile()
		case <-s.stopChan:
			log.Println("[ReconcileScheduler] Stopped")
			return
		}
	}
}

// Stop stops the reconcile scheduler
func (s *ReconcileScheduler) Stop() {
	close(s.stopChan)
}

// scheduleReconcile creates a reconcile event
func (s *ReconcileScheduler) scheduleReconcile() {
	log.Println("[ReconcileScheduler] Scheduling reconcile event")

	// Create context with event data
	ctx := events.NewReconcileContext()

	// Create event (without deadline for reconcile)
	event := eventqueue.NewEvent(string(events.EventReconcile), ctx)

	// Enqueue event
	err := s.eventQueue.Enqueue(event)
	if err != nil {
		log.Printf("[ReconcileScheduler] Failed to enqueue reconcile event: %v", err)
	}
}
