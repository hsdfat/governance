package main

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/chronnie/governance/manager"
	"github.com/chronnie/governance/models"
)

func main() {
	// Create manager with default in-memory storage
	managerConfig := &models.ManagerConfig{
		ServerPort:           8080,
		HealthCheckInterval:  30 * time.Second,
		NotificationInterval: 60 * time.Second,
		HealthCheckTimeout:   5 * time.Second,
		NotificationTimeout:  5 * time.Second,
		HealthCheckRetry:     3,
		EventQueueSize:       1000,
	}

	mgr := manager.NewManager(managerConfig)

	// Start manager
	if err := mgr.Start(); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}

	// Give the manager time to start
	time.Sleep(100 * time.Millisecond)

	// Simulate registering some services
	fmt.Println("=== Simulating Service Registrations ===")
	simulateRegistrations(mgr)

	// Wait for registrations to be processed
	time.Sleep(200 * time.Millisecond)

	// Query pods by service group
	fmt.Println("\n=== Query Pods by Service Group ===")
	queryPodsByServiceGroup(mgr, "user-service")
	queryPodsByServiceGroup(mgr, "order-service")
	queryPodsByServiceGroup(mgr, "payment-service")

	// Query all service groups and their pods
	fmt.Println("\n=== All Service Groups and Pods ===")
	queryAllServicePods(mgr)

	// Stop manager
	if err := mgr.Stop(); err != nil {
		log.Printf("Error stopping manager: %v", err)
	}
}

func simulateRegistrations(mgr *manager.Manager) {
	// Get registry directly (this is for example purposes)
	registry := mgr.GetRegistry()

	// Register user-service pods
	registry.Register(&models.ServiceRegistration{
		ServiceName: "user-service",
		PodName:     "user-pod-1",
		Providers: []models.ProviderInfo{
			{Protocol: models.ProtocolHTTP, IP: "192.168.1.10", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.10:8080/health",
		NotificationURL: "http://192.168.1.10:8080/notify",
		Subscriptions: []models.Subscription{{ServiceName: "order-service"}},
	})

	registry.Register(&models.ServiceRegistration{
		ServiceName: "user-service",
		PodName:     "user-pod-2",
		Providers: []models.ProviderInfo{
			{Protocol: models.ProtocolHTTP, IP: "192.168.1.11", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.11:8080/health",
		NotificationURL: "http://192.168.1.11:8080/notify",
		Subscriptions: []models.Subscription{{ServiceName: "order-service"}},
	})

	// Register order-service pods
	registry.Register(&models.ServiceRegistration{
		ServiceName: "order-service",
		PodName:     "order-pod-1",
		Providers: []models.ProviderInfo{
			{Protocol: models.ProtocolHTTP, IP: "192.168.1.20", Port: 8080},
			{Protocol: models.ProtocolTCP, IP: "192.168.1.20", Port: 9090},
		},
		HealthCheckURL:  "http://192.168.1.20:8080/health",
		NotificationURL: "http://192.168.1.20:8080/notify",
		Subscriptions: []models.Subscription{{ServiceName: "payment-service"}},
	})

	registry.Register(&models.ServiceRegistration{
		ServiceName: "order-service",
		PodName:     "order-pod-2",
		Providers: []models.ProviderInfo{
			{Protocol: models.ProtocolHTTP, IP: "192.168.1.21", Port: 8080},
			{Protocol: models.ProtocolTCP, IP: "192.168.1.21", Port: 9090},
		},
		HealthCheckURL:  "http://192.168.1.21:8080/health",
		NotificationURL: "http://192.168.1.21:8080/notify",
		Subscriptions: []models.Subscription{{ServiceName: "payment-service"}},
	})

	registry.Register(&models.ServiceRegistration{
		ServiceName: "order-service",
		PodName:     "order-pod-3",
		Providers: []models.ProviderInfo{
			{Protocol: models.ProtocolHTTP, IP: "192.168.1.22", Port: 8080},
			{Protocol: models.ProtocolTCP, IP: "192.168.1.22", Port: 9090},
		},
		HealthCheckURL:  "http://192.168.1.22:8080/health",
		NotificationURL: "http://192.168.1.22:8080/notify",
		Subscriptions: []models.Subscription{{ServiceName: "payment-service"}},
	})

	// Register payment-service pod
	registry.Register(&models.ServiceRegistration{
		ServiceName: "payment-service",
		PodName:     "payment-pod-1",
		Providers: []models.ProviderInfo{
			{Protocol: models.ProtocolHTTP, IP: "192.168.1.30", Port: 8080},
		},
		HealthCheckURL:  "http://192.168.1.30:8080/health",
		NotificationURL: "http://192.168.1.30:8080/notify",
		Subscriptions: []models.Subscription{},
	})

	fmt.Println("Registered services:")
	fmt.Println("  - user-service: 2 pods")
	fmt.Println("  - order-service: 3 pods")
	fmt.Println("  - payment-service: 1 pod")
}

func queryPodsByServiceGroup(mgr *manager.Manager, serviceName string) {
	pods := mgr.GetServicePods(serviceName)

	fmt.Printf("\nService: %s (Total Pods: %d)\n", serviceName, len(pods))
	for i, pod := range pods {
		fmt.Printf("  Pod %d: %s\n", i+1, pod.PodName)
		fmt.Printf("    Status: %s\n", pod.Status)
		fmt.Printf("    Registered At: %s\n", pod.RegisteredAt.Format(time.RFC3339))
		fmt.Printf("    Providers:\n")
		for _, provider := range pod.Providers {
			fmt.Printf("      - %s://%s:%d\n", provider.Protocol, provider.IP, provider.Port)
		}
		if len(pod.Subscriptions) > 0 {
			fmt.Printf("    Subscriptions: %v\n", pod.Subscriptions)
		}
	}
}

func queryAllServicePods(mgr *manager.Manager) {
	allServicePods := mgr.GetAllServicePods()

	fmt.Printf("Total Service Groups: %d\n\n", len(allServicePods))

	for serviceName, pods := range allServicePods {
		fmt.Printf("Service: %s\n", serviceName)
		fmt.Printf("  Total Pods: %d\n", len(pods))
		fmt.Printf("  Pods:\n")
		for _, pod := range pods {
			fmt.Printf("    - %s (%s, %s)\n",
				pod.PodName,
				pod.Providers[0].IP,
				pod.Status,
			)
		}
		fmt.Println()
	}

	// Example: Export to JSON
	fmt.Println("=== JSON Export ===")
	jsonData, err := json.MarshalIndent(allServicePods, "", "  ")
	if err != nil {
		log.Printf("Failed to marshal to JSON: %v", err)
		return
	}
	fmt.Println(string(jsonData))
}
