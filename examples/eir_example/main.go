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
	log.Println("Starting EIR service example with multiple providers...")

	// Configuration (normally would come from environment variables)
	managerURL := "http://localhost:8080"
	serviceName := "eir-service"
	podName := "eir-pod-1"
	podIP := "192.168.1.20"
	httpPort := 8080
	diameterPort := 3868

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
			for _, provider := range pod.Providers {
				log.Printf("       * Provider: %s (%s) at %s:%d",
					provider.ProviderID, provider.Protocol, provider.IP, provider.Port)
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

	// Register EIR service with MULTIPLE providers (diameter + HTTP)
	registration := &models.ServiceRegistration{
		ServiceName: serviceName,
		PodName:     podName,
		Providers: []models.ProviderInfo{
			{
				ProviderID: string(models.ProviderEIRDiameter), // Diameter interface
				Protocol:   models.ProtocolTCP,
				IP:         podIP,
				Port:       diameterPort,
			},
			{
				ProviderID: string(models.ProviderEIRHTTP), // HTTP API interface
				Protocol:   models.ProtocolHTTP,
				IP:         podIP,
				Port:       httpPort,
			},
		},
		HealthCheckURL:  notifServer.GetHealthCheckURL(podIP),
		NotificationURL: notifServer.GetNotificationURL(podIP),
		Subscriptions: []models.Subscription{
			{
				ServiceName: "smf-service",
				ProviderIDs: []string{string(models.ProviderSMFPFCP)}, // Only subscribe to PFCP endpoints
			},
			{
				ServiceName: "upf-service", // Subscribe to all UPF providers
			},
		},
	}

	resp, err := govClient.Register(registration)
	if err != nil {
		log.Fatalf("Failed to register: %v", err)
	}

	log.Printf("EIR Service registered successfully!")
	log.Printf("  - Service: %s", serviceName)
	log.Printf("  - Pod: %s", podName)
	log.Printf("  - Providers:")
	for _, p := range registration.Providers {
		log.Printf("    * %s (%s) at %s:%d", p.ProviderID, p.Protocol, p.IP, p.Port)
	}
	log.Printf("  - Health Check URL: %s", registration.HealthCheckURL)
	log.Printf("  - Notification URL: %s", registration.NotificationURL)
	log.Printf("  - Subscriptions:")
	for _, sub := range registration.Subscriptions {
		if len(sub.ProviderIDs) > 0 {
			log.Printf("    * %s (providers: %v)", sub.ServiceName, sub.ProviderIDs)
		} else {
			log.Printf("    * %s (all providers)", sub.ServiceName)
		}
	}

	log.Printf("\nPods for service '%s': %d", resp.ServiceName, len(resp.Pods))
	for _, pod := range resp.Pods {
		log.Printf("  * Pod: %s, Status: %s, Providers: %d",
			pod.PodName, pod.Status, len(pod.Providers))
		for _, provider := range pod.Providers {
			log.Printf("    - %s (%s) at %s:%d",
				provider.ProviderID, provider.Protocol, provider.IP, provider.Port)
		}
	}

	if len(resp.SubscribedServices) > 0 {
		log.Printf("\nSubscribed Services (received current pod info):")
		for svcName, pods := range resp.SubscribedServices {
			log.Printf("  - %s: %d pods", svcName, len(pods))
			for _, pod := range pods {
				log.Printf("      * Pod: %s, Status: %s, Providers: %d",
					pod.PodName, pod.Status, len(pod.Providers))
				for _, provider := range pod.Providers {
					log.Printf("        - %s (%s) at %s:%d",
						provider.ProviderID, provider.Protocol, provider.IP, provider.Port)
				}
			}
		}
	} else {
		log.Printf("\nNo active pods found for subscribed services yet")
	}

	// Demonstrate provider-specific endpoint queries
	log.Printf("\n=== Demonstrating Provider-Specific Queries ===")

	// Get specific provider endpoints from SMF (PFCP only, as subscribed)
	if smfPfcpEndpoints := govClient.GetProviderEndpoints("smf-service", string(models.ProviderSMFPFCP)); len(smfPfcpEndpoints) > 0 {
		log.Printf("\nSMF PFCP Endpoints (filtered subscription):")
		for _, ep := range smfPfcpEndpoints {
			log.Printf("  - Pod: %s, Provider: %s, %s://%s:%d [%s]",
				ep.PodName, ep.ProviderID, ep.Protocol, ep.IP, ep.Port, ep.Status)
		}
	} else {
		log.Printf("\nNo SMF PFCP endpoints available yet")
	}

	// Get all provider endpoints from UPF
	if upfEndpoints := govClient.GetAllProviderEndpoints("upf-service"); len(upfEndpoints) > 0 {
		log.Printf("\nUPF All Provider Endpoints:")
		for providerID, endpoints := range upfEndpoints {
			log.Printf("  Provider: %s", providerID)
			for _, ep := range endpoints {
				log.Printf("    - Pod: %s, %s://%s:%d [%s]",
					ep.PodName, ep.Protocol, ep.IP, ep.Port, ep.Status)
			}
		}
	} else {
		log.Printf("\nNo UPF endpoints available yet")
	}

	log.Printf("\n=== Pod Info will be automatically updated when notifications arrive ===\n")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	log.Println("Shutting down EIR service...")

	// Unregister from manager
	if err := govClient.Unregister(); err != nil {
		log.Printf("Failed to unregister: %v", err)
	} else {
		log.Println("EIR Service unregistered successfully")
	}

	// Stop notification server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := notifServer.Stop(ctx); err != nil {
		log.Printf("Failed to stop notification server: %v", err)
	}

	log.Println("EIR Service stopped successfully")
}
