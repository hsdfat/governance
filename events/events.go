package events

import (
	"context"

	"github.com/chronnie/governance/models"
)

// EventName represents the type of event in the system
type EventName string

const (
	EventRegister    EventName = "register"
	EventUnregister  EventName = "unregister"
	EventHealthCheck EventName = "health_check"
	EventReconcile   EventName = "reconcile"
)

// Context keys for event data
type contextKey string

const (
	ContextKeyEventData contextKey = "event_data"
)

// RegisterEvent is triggered when a service registers
type RegisterEvent struct {
	Registration *models.ServiceRegistration
	ResultChan   chan *RegisterResult // Channel to receive registration result
}

// RegisterResult contains the result of a registration
type RegisterResult struct {
	Pods               []models.PodInfo
	SubscribedServices map[string][]models.PodInfo
	Error              error
}

func (e *RegisterEvent) GetName() EventName {
	return EventRegister
}

func (e *RegisterEvent) HasDeadline() bool {
	return true // Register events have deadline
}

// UnregisterEvent is triggered when a service unregisters
type UnregisterEvent struct {
	ServiceName string
	PodName     string
}

func (e *UnregisterEvent) GetName() EventName {
	return EventUnregister
}

func (e *UnregisterEvent) HasDeadline() bool {
	return true // Unregister events have deadline
}

// HealthCheckEvent is triggered to check service health
type HealthCheckEvent struct {
	ServiceKey string // format: service_name:pod_name
}

func (e *HealthCheckEvent) GetName() EventName {
	return EventHealthCheck
}

func (e *HealthCheckEvent) HasDeadline() bool {
	return false // Health check events don't have deadline
}

// ReconcileEvent is triggered to notify all subscribers with current state
type ReconcileEvent struct {
	// Empty struct - triggers full system reconciliation
}

func (e *ReconcileEvent) GetName() EventName {
	return EventReconcile
}

func (e *ReconcileEvent) HasDeadline() bool {
	return false // Reconcile events don't have deadline
}

// Helper functions to create context with event data

// NewRegisterContext creates a context with RegisterEvent data
func NewRegisterContext(registration *models.ServiceRegistration) context.Context {
	return NewRegisterContextWithResult(registration, nil)
}

// NewRegisterContextWithResult creates a context with RegisterEvent data and result channel
func NewRegisterContextWithResult(registration *models.ServiceRegistration, resultChan chan *RegisterResult) context.Context {
	return context.WithValue(context.Background(), ContextKeyEventData, &RegisterEvent{
		Registration: registration,
		ResultChan:   resultChan,
	})
}

// NewUnregisterContext creates a context with UnregisterEvent data
func NewUnregisterContext(serviceName, podName string) context.Context {
	return context.WithValue(context.Background(), ContextKeyEventData, &UnregisterEvent{
		ServiceName: serviceName,
		PodName:     podName,
	})
}

// NewHealthCheckContext creates a context with HealthCheckEvent data
func NewHealthCheckContext(serviceKey string) context.Context {
	return context.WithValue(context.Background(), ContextKeyEventData, &HealthCheckEvent{
		ServiceKey: serviceKey,
	})
}

// NewReconcileContext creates a context with ReconcileEvent data
func NewReconcileContext() context.Context {
	return context.WithValue(context.Background(), ContextKeyEventData, &ReconcileEvent{})
}

// GetEventData extracts event data from context
func GetEventData(ctx context.Context) interface{} {
	return ctx.Value(ContextKeyEventData)
}
