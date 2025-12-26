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

// FilterProviders returns a new PodInfo with only the specified provider IDs
// If providerIDs is empty, returns all providers
func (p *PodInfo) FilterProviders(providerIDs []string) PodInfo {
	if len(providerIDs) == 0 {
		return *p
	}

	filtered := PodInfo{
		PodName:   p.PodName,
		Status:    p.Status,
		Providers: make([]ProviderInfo, 0),
	}

	// Create a set for fast lookup
	providerSet := make(map[string]bool)
	for _, id := range providerIDs {
		providerSet[id] = true
	}

	// Filter providers
	for _, provider := range p.Providers {
		if providerSet[provider.ProviderID] {
			filtered.Providers = append(filtered.Providers, provider)
		}
	}

	return filtered
}

// NotificationPayload is sent to subscribers when service changes occur
type NotificationPayload struct {
	ServiceName string      `json:"service_name"`
	EventType   EventType   `json:"event_type"`
	Timestamp   time.Time   `json:"timestamp"`
	Pods        []PodInfo   `json:"pods"`
}
