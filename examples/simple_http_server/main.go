package main

import (
	"log"
	"time"

	"github.com/chronnie/governance/client"
	"github.com/chronnie/governance/models"
)

func main() {
	log.Println("=== Simple HTTP Server with Subscribed Services Example ===\n")

	// Configuration
	serviceName := "my-service"
	podName := "my-pod-1"
	httpPort := 9090

	// Create governance client
	govClient := client.NewClient(&client.ClientConfig{
		ManagerURL:  "http://localhost:8080",
		ServiceName: serviceName,
		PodName:     podName,
		Timeout:     10 * time.Second,
	})

	// Simulate some subscribed services for demonstration
	log.Println("Simulating subscribed services...")
	govClient.UpdatePodInfo("order-service", []models.PodInfo{
		{PodName: "order-pod-1", Status: models.StatusHealthy},
		{PodName: "order-pod-2", Status: models.StatusHealthy},
	})

	govClient.UpdatePodInfo("payment-service", []models.PodInfo{
		{PodName: "payment-pod-1", Status: models.StatusHealthy},
	})

	// Create notification handler
	notificationHandler := func(payload *models.NotificationPayload) {
		log.Printf("Notification received: service=%s, event=%s, pods=%d",
			payload.ServiceName, payload.EventType, len(payload.Pods))
	}

	// Start HTTP server - subscribed services endpoint is automatically registered!
	log.Printf("Starting HTTP server on port %d...\n", httpPort)

	config := client.HTTPServerConfig{
		Port:                httpPort,
		NotificationHandler: govClient.WrapNotificationHandler(notificationHandler),
	}

	log.Println("Available endpoints:")
	log.Printf("  - GET  /health              - Health check")
	log.Printf("  - GET  /subscribed-services - List subscribed services (automatically added)")
	log.Printf("  - POST /notify              - Receive notifications")
	log.Printf("  - POST /heartbeat           - Receive heartbeat")
	log.Println()
	log.Printf("Test with: curl http://localhost:%d/subscribed-services | jq\n", httpPort)

	// Use client method to start server with subscribed services endpoint
	if err := govClient.StartHTTPServerWithClient(config); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
