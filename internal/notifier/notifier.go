package notifier

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/pkg/logger"
)

// Notifier handles sending notifications to subscribers
type Notifier struct {
	httpClient *http.Client
	timeout    time.Duration
	logger     logger.Logger
}

// NewNotifier creates a new notifier with given timeout
func NewNotifier(timeout time.Duration, log logger.Logger) *Notifier {
	return &Notifier{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout: timeout,
		logger:  log,
	}
}

// NotifySubscribers sends notification to all subscribers
// Does not retry on failure as per requirements
func (n *Notifier) NotifySubscribers(subscribers []*models.ServiceInfo, payload *models.NotificationPayload) {
	n.logger.Debugw("Notifier: NotifySubscribers called",
		"subscriber_count", len(subscribers),
		"event_type", string(payload.EventType),
		"service_name", payload.ServiceName,
	)

	for _, subscriber := range subscribers {
		n.logger.Debugw("Notifier: Sending notification to subscriber",
			"subscriber_key", subscriber.GetKey(),
			"notification_url", subscriber.NotificationURL,
			"event_type", string(payload.EventType),
		)
		go n.sendNotification(subscriber.NotificationURL, payload, subscriber.GetKey())
	}
}

// NotifySubscriber sends notification to a single subscriber
func (n *Notifier) NotifySubscriber(notificationURL string, payload *models.NotificationPayload) {
	n.logger.Debugw("Notifier: NotifySubscriber called",
		"notification_url", notificationURL,
		"event_type", string(payload.EventType),
	)
	go n.sendNotification(notificationURL, payload, "")
}

// sendNotification sends HTTP POST notification to a URL
func (n *Notifier) sendNotification(url string, payload *models.NotificationPayload, subscriberKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), n.timeout)
	defer cancel()

	logArgs := []interface{}{
		"notification_url", url,
		"event_type", string(payload.EventType),
		"service_name", payload.ServiceName,
	}
	if subscriberKey != "" {
		logArgs = append(logArgs, "subscriber_key", subscriberKey)
	}

	n.logger.Debugw("Notifier: Sending HTTP POST notification", logArgs...)

	// Marshal payload to JSON
	jsonData, err := json.Marshal(payload)
	if err != nil {
		n.logger.Errorw("Notifier: Failed to marshal notification payload",
			append(logArgs, "error", err)...)
		return
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		n.logger.Errorw("Notifier: Failed to create notification request",
			append(logArgs, "error", err)...)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := n.httpClient.Do(req)
	if err != nil {
		n.logger.Errorw("Notifier: Failed to send notification",
			append(logArgs, "error", err)...)
		return
	}
	defer resp.Body.Close()

	// Check response status
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		n.logger.Warnw("Notifier: Notification returned non-success status",
			append(logArgs, "status_code", resp.StatusCode)...)
		return
	}

	n.logger.Infow("Notifier: Successfully sent notification",
		append(logArgs, "status_code", resp.StatusCode)...)
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
	logger     logger.Logger
}

// NewHealthChecker creates a new health checker
func NewHealthChecker(timeout time.Duration, maxRetries int, log logger.Logger) *HealthChecker {
	return &HealthChecker{
		httpClient: &http.Client{
			Timeout: timeout,
		},
		timeout:    timeout,
		maxRetries: maxRetries,
		logger:     log,
	}
}

// CheckHealth performs health check with retries
// Returns true if healthy, false if unhealthy
func (hc *HealthChecker) CheckHealth(healthCheckURL string) bool {
	hc.logger.Debugw("HealthChecker: Starting health check",
		"health_check_url", healthCheckURL,
		"max_retries", hc.maxRetries,
		"timeout", hc.timeout,
	)

	for attempt := 0; attempt <= hc.maxRetries; attempt++ {
		if attempt > 0 {
			// Exponential backoff: 1s, 2s, 4s...
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			hc.logger.Debugw("HealthChecker: Retrying after backoff",
				"health_check_url", healthCheckURL,
				"attempt", attempt,
				"max_retries", hc.maxRetries,
				"backoff", backoff,
			)
			time.Sleep(backoff)
		}

		ctx, cancel := context.WithTimeout(context.Background(), hc.timeout)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthCheckURL, nil)
		if err != nil {
			cancel()
			hc.logger.Errorw("HealthChecker: Failed to create health check request",
				"health_check_url", healthCheckURL,
				"attempt", attempt+1,
				"error", err,
			)
			continue
		}

		resp, err := hc.httpClient.Do(req)
		cancel()

		if err != nil {
			hc.logger.Warnw("HealthChecker: Health check request failed",
				"health_check_url", healthCheckURL,
				"attempt", attempt+1,
				"total_attempts", hc.maxRetries+1,
				"error", err,
			)
			continue
		}

		resp.Body.Close()

		// Consider 2xx as healthy
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			hc.logger.Debugw("HealthChecker: Health check passed",
				"health_check_url", healthCheckURL,
				"status_code", resp.StatusCode,
				"attempt", attempt+1,
			)
			return true
		}

		hc.logger.Warnw("HealthChecker: Health check returned unhealthy status",
			"health_check_url", healthCheckURL,
			"attempt", attempt+1,
			"total_attempts", hc.maxRetries+1,
			"status_code", resp.StatusCode,
		)
	}

	hc.logger.Errorw("HealthChecker: Health check failed after all retries",
		"health_check_url", healthCheckURL,
		"total_attempts", hc.maxRetries+1,
	)
	return false
}

// GetHealthStatus performs health check and returns status
func (hc *HealthChecker) GetHealthStatus(healthCheckURL string) models.ServiceStatus {
	if hc.CheckHealth(healthCheckURL) {
		return models.StatusHealthy
	}
	return models.StatusUnhealthy
}
