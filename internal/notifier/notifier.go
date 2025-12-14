package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/chronnie/governance/models"
)

// Notifier handles sending notifications to subscribers
type Notifier struct {
	httpClient *http.Client
	timeout    time.Duration
}

// NewNotifier creates a new notifier with given timeout
func NewNotifier(timeout time.Duration) *Notifier {
	return &Notifier{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
	}
}

// NotifySubscribers sends notification to all subscribers
// Does not retry on failure as per requirements
func (n *Notifier) NotifySubscribers(subscribers []*models.ServiceInfo, payload *models.NotificationPayload) {
	for _, subscriber := range subscribers {
		go n.sendNotification(subscriber.NotificationURL, payload)
	}
}

// NotifySubscriber sends notification to a single subscriber
func (n *Notifier) NotifySubscriber(notificationURL string, payload *models.NotificationPayload) {
	go n.sendNotification(notificationURL, payload)
}

// sendNotification sends HTTP POST notification to a URL
func (n *Notifier) sendNotification(url string, payload *models.NotificationPayload) {
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()

	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[Notifier] Failed to marshal notification payload: %v", err)
		return
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Printf("[Notifier] Failed to create notification request to %s: %v", url, err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := n.httpClient.Do(req)
	if err != nil {
		log.Printf("[Notifier] Failed to send notification to %s: %v", url, err)
		return
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[Notifier] Notification to %s returned non-success status: %d", url, resp.StatusCode)
		return
	}

	log.Printf("[Notifier] Successfully sent notification to %s (event: %s, service: %s)",
		url, payload.EventType, payload.ServiceName)
}

// BuildNotificationPayload creates a notification payload from service pods
func BuildNotificationPayload(serviceName string, eventType models.EventType, pods []*models.ServiceInfo) *models.NotificationPayload {
	podInfos := make([]models.PodInfo, 0, len(pods))

	for _, pod := range pods {
		podInfos = append(podInfos, models.PodInfo{
			PodName:   pod.PodName,
			Status:    pod.Status,
			Providers: pod.Providers,
		})
	}

	return &models.NotificationPayload{
		ServiceName: serviceName,
		EventType:   eventType,
		Timestamp:   time.Now(),
		Pods:        podInfos,
	}
}

// HealthChecker performs health checks on services
type HealthChecker struct {
	httpClient *http.Client
	timeout    time.Duration
	maxRetries int
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(timeout time.Duration, maxRetries int) *HealthChecker {
	return &HealthChecker{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout:    timeout,
		maxRetries: maxRetries,
	}
}

// CheckHealth performs health check with retries
// Returns true if healthy, false if unhealthy
func (hc *HealthChecker) CheckHealth(healthCheckURL string) bool {
	for attempt := 0; attempt <= hc.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			log.Printf("[HealthChecker] Retry %d/%d for %s after %v", attempt, hc.maxRetries, healthCheckURL, backoff)
			time.Sleep(backoff)
		}

		ctx, cancel := context.WithTimeout(context.Background(), hc.timeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthCheckURL, nil)
		if err != nil {
			cancel()
			log.Printf("[HealthChecker] Failed to create health check request for %s: %v", healthCheckURL, err)
			continue
		}

		resp, err := hc.httpClient.Do(req)
		cancel()

		if err != nil {
			log.Printf("[HealthChecker] Health check failed for %s (attempt %d/%d): %v",
				healthCheckURL, attempt+1, hc.maxRetries+1, err)
			continue
		}

		resp.Body.Close()

		// Consider 2xx as healthy
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			log.Printf("[HealthChecker] Health check passed for %s (status: %d)", healthCheckURL, resp.StatusCode)
			return true
		}

		log.Printf("[HealthChecker] Health check failed for %s (attempt %d/%d, status: %d)",
			healthCheckURL, attempt+1, hc.maxRetries+1, resp.StatusCode)
	}

	log.Printf("[HealthChecker] Health check failed for %s after %d attempts", healthCheckURL, hc.maxRetries+1)
	return false
}

// GetHealthStatus performs health check and returns status
func (hc *HealthChecker) GetHealthStatus(healthCheckURL string) models.ServiceStatus {
	if hc.CheckHealth(healthCheckURL) {
		return models.StatusHealthy
	}
	return models.StatusUnhealthy
}
