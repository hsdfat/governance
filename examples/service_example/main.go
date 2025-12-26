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

	// Create governance client
	govClient := client.NewClient(&client.ClientConfig{
		ManagerURL:  managerURL,
		ServiceName: serviceName,
		PodName:     podName,
		Timeout:     10 * time.Second,
	})

	// Create notification handler
	notificationHandler := func(payload *models.NotificationPayload) {
		log.Printf("===> Received notification: service=%s, event=%s, pods=%d",
			payload.ServiceName, payload.EventType, len(payload.Pods))

		for _, pod := range payload.Pods {
			log.Printf("     - Pod: %s, Status: %s, Providers: %d",
				pod.PodName, pod.Status, len(pod.Providers))
		}

		// Demonstrate accessing stored pod info
		log.Printf("\n===> Current stored pod info after notification:")
		if payload.ServiceName == serviceName {
			ownPods := govClient.GetOwnPods()
			log.Printf("     Own service pods: %d", len(ownPods))
		} else {
			subscribedPods, exists := govClient.GetSubscribedServicePods(payload.ServiceName)
			if exists {
				log.Printf("     Subscribed service '%s' pods: %d", payload.ServiceName, len(subscribedPods))
			}
		}
	}

	// Wrap the handler to automatically update stored pod info
	wrappedHandler := govClient.WrapNotificationHandler(notificationHandler)

	// Create and start notification server with wrapped handler
	notifServer := client.NewNotificationServer(httpPort, wrappedHandler)
	go func() {
		if err := notifServer.Start(); err != nil {
			log.Printf("Notification server error: %v", err)
		}
	}()

	// Wait a bit for server to start
	time.Sleep(1 * time.Second)

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

	resp, err := govClient.Register(registration)
	if err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	log.Printf("Service registered successfully!")
	log.Printf("  - Service: %s", serviceName)
	log.Printf("  - Pod: %s", podName)
	log.Printf("  - Health Check URL: %s", registration.HealthCheckURL)
	log.Printf("  - Notification URL: %s", registration.NotificationURL)
	log.Printf("  - Subscriptions: %v", registration.Subscriptions)
	log.Printf("\nPods for service '%s': %d", resp.ServiceName, len(resp.Pods))
	for _, pod := range resp.Pods {
		log.Printf("  * Pod: %s, Status: %s, Providers: %d",
			pod.PodName, pod.Status, len(pod.Providers))
	}

	if len(resp.SubscribedServices) > 0 {
		log.Printf("\nSubscribed Services (received current pod info):")
		for serviceName, pods := range resp.SubscribedServices {
			log.Printf("  - %s: %d pods", serviceName, len(pods))
			for _, pod := range pods {
				log.Printf("      * Pod: %s, Status: %s, Providers: %d",
					pod.PodName, pod.Status, len(pod.Providers))
			}
		}
	} else {
		log.Printf("\nNo active pods found for subscribed services yet")
	}

	// Demonstrate accessing stored pod info at any time
	log.Printf("\n=== Demonstrating Pod Info Access ===")

	// Access own service pods
	ownPods := govClient.GetOwnPods()
	log.Printf("\nAccessing own pods via GetOwnPods():")
	log.Printf("  Found %d pods for service '%s'", len(ownPods), serviceName)
	for _, pod := range ownPods {
		log.Printf("    - %s [%s]", pod.PodName, pod.Status)
	}

	// Access specific subscribed service
	if pods, exists := govClient.GetSubscribedServicePods("order-service"); exists {
		log.Printf("\nAccessing subscribed service via GetSubscribedServicePods('order-service'):")
		log.Printf("  Found %d pods for 'order-service'", len(pods))
		for _, pod := range pods {
			log.Printf("    - %s [%s]", pod.PodName, pod.Status)
		}
	} else {
		log.Printf("\nNo pods found for 'order-service' (service may not be registered yet)")
	}

	// Access all subscribed services
	allSubscribed := govClient.GetAllSubscribedServices()
	log.Printf("\nAccessing all subscribed services via GetAllSubscribedServices():")
	log.Printf("  Total subscribed services with active pods: %d", len(allSubscribed))
	for svcName, pods := range allSubscribed {
		log.Printf("    - %s: %d pods", svcName, len(pods))
	}

	log.Printf("\n=== Pod Info will be automatically updated when notifications arrive ===\n")

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
