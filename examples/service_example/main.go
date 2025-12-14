package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chronnie/governance/client"
	"github.com/chronnie/governance/models"
)

func main() {
	log.Println("Starting service example...")

	// Configuration (normally would come from environment variables)
	managerURL := "http://localhost:8080"
	serviceName := "user-service"
	podName := "user-service-pod-1"
	podIP := "192.168.1.10"
	httpPort := 9001

	// Create notification handler
	notificationHandler := func(payload *models.NotificationPayload) {
		log.Printf("===> Received notification: service=%s, event=%s, pods=%d",
			payload.ServiceName, payload.EventType, len(payload.Pods))

		for _, pod := range payload.Pods {
			log.Printf("     - Pod: %s, Status: %s, Providers: %d",
				pod.PodName, pod.Status, len(pod.Providers))
		}
	}

	// Create and start notification server
	notifServer := client.NewNotificationServer(httpPort, notificationHandler)
	go func() {
		if err := notifServer.Start(); err != nil {
			log.Printf("Notification server error: %v", err)
		}
	}()

	// Wait a bit for server to start
	time.Sleep(1 * time.Second)

	// Create governance client
	govClient := client.NewClient(&client.ClientConfig{
		ManagerURL:  managerURL,
		ServiceName: serviceName,
		PodName:     podName,
		Timeout:     10 * time.Second,
	})

	// Register this service with the manager
	registration := &models.ServiceRegistration{
		ServiceName: serviceName,
		PodName:     podName,
		Providers: []models.ProviderInfo{
			{
				Protocol: models.ProtocolHTTP,
				IP:       podIP,
				Port:     httpPort,
			},
		},
		HealthCheckURL:  notifServer.GetHealthCheckURL(podIP),
		NotificationURL: notifServer.GetNotificationURL(podIP),
		Subscriptions:   []string{"order-service", "payment-service"}, // Subscribe to these services
	}

	if err := govClient.Register(registration); err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	log.Printf("Service registered successfully!")
	log.Printf("  - Service: %s", serviceName)
	log.Printf("  - Pod: %s", podName)
	log.Printf("  - Health Check URL: %s", registration.HealthCheckURL)
	log.Printf("  - Notification URL: %s", registration.NotificationURL)
	log.Printf("  - Subscriptions: %v", registration.Subscriptions)

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	log.Println("Shutting down service...")

	// Unregister from manager
	if err := govClient.Unregister(); err != nil {
		log.Printf("Failed to unregister: %v", err)
	} else {
		log.Println("Service unregistered successfully")
	}

	// Stop notification server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := notifServer.Stop(ctx); err != nil {
		log.Printf("Failed to stop notification server: %v", err)
	}

	log.Println("Service stopped successfully")
}
