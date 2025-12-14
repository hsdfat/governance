package models

import "time"

// EventType represents the type of notification event
type EventType string

const (
	EventTypeRegister   EventType = "register"
	EventTypeUnregister EventType = "unregister"
	EventTypeUpdate     EventType = "update"
	EventTypeReconcile  EventType = "reconcile"
)

// PodInfo represents information about a pod in the notification
type PodInfo struct {
	PodName   string         `json:"pod_name"`
	Status    ServiceStatus  `json:"status"`
	Providers []ProviderInfo `json:"providers"`
}

// NotificationPayload is sent to subscribers when service changes occur
type NotificationPayload struct {
	ServiceName string      `json:"service_name"`
	EventType   EventType   `json:"event_type"`
	Timestamp   time.Time   `json:"timestamp"`
	Pods        []PodInfo   `json:"pods"`
}
