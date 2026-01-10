# Governance Library

A Go library for service discovery and management in microservices architectures. This library provides a centralized governance manager that tracks service registrations, performs health checks, and notifies subscribers of service changes.

## Features

- **Service Registration**: Services register themselves with protocol endpoints (HTTP, TCP, PFCP, GTP, UDP)
- **Subscription System**: Services can subscribe to other service groups for change notifications
- **Health Checking**: Automatic periodic health checks with retry mechanism
- **Event-Driven Architecture**: Uses [go-event-queue](https://github.com/hsdfat/telco/equeue) for lock-free event processing
- **Automatic Notifications**: Subscribers are notified when services register, unregister, or change health status
- **Periodic Reconciliation**: Regular full-state notifications to all subscribers

## Architecture

The library consists of two main components:

### Manager
The centralized governance service that:
- Exposes REST API for service registration/unregistration
- Maintains in-memory registry of all services
- Performs periodic health checks on registered services
- Processes events through a single-worker event queue (FIFO)
- Sends notifications to subscribers when changes occur

### Client
Helper library for services to:
- Register/unregister with the manager
- Receive notifications via HTTP callbacks
- Handle health check requests

## Installation

```bash
go get github.com/chronnie/governance
```

## Quick Start

### Running the Manager

```go
package main

import (
    "github.com/chronnie/governance/manager"
    "github.com/chronnie/governance/models"
    "time"
)

func main() {
    config := &models.ManagerConfig{
        ServerPort:           8080,
        HealthCheckInterval:  30 * time.Second,
        HealthCheckTimeout:   5 * time.Second,
        HealthCheckRetry:     3,
        NotificationInterval: 60 * time.Second,
        NotificationTimeout:  5 * time.Second,
        EventQueueSize:       1000,
    }

    mgr := manager.NewManager(config)
    mgr.Start()

    // Wait for shutdown signal...
    mgr.Wait()
}
```

### Registering a Service

```go
package main

import (
    "github.com/chronnie/governance/client"
    "github.com/chronnie/governance/models"
)

func main() {
    // Create notification handler
    handler := func(payload *models.NotificationPayload) {
        // Handle notification about service changes
        log.Printf("Service %s changed: %v", payload.ServiceName, payload.Pods)
    }

    // Start notification server
    notifServer := client.NewNotificationServer(9001, handler)
    go notifServer.Start()

    // Create client
    govClient := client.NewClient(&client.ClientConfig{
        ManagerURL:  "http://localhost:8080",
        ServiceName: "user-service",
        PodName:     "user-service-pod-1",
    })

    // Register
    registration := &models.ServiceRegistration{
        ServiceName: "user-service",
        PodName:     "user-service-pod-1",
        Providers: []models.ProviderInfo{
            {
                Protocol: models.ProtocolHTTP,
                IP:       "192.168.1.10",
                Port:     9001,
            },
        },
        HealthCheckURL:  "http://192.168.1.10:9001/health",
        NotificationURL: "http://192.168.1.10:9001/notify",
        Subscriptions:   []string{"order-service", "payment-service"},
    }

    govClient.Register(registration)

    // Your service logic here...

    // Unregister on shutdown
    govClient.Unregister()
}
```

## API Reference

### Manager REST API

#### Register Service
```
POST /register

{
  "service_name": "user-service",
  "pod_name": "user-service-pod-1",
  "providers": [
    {
      "protocol": "http",
      "ip": "192.168.1.10",
      "port": 8080
    }
  ],
  "health_check_url": "http://192.168.1.10:8080/health",
  "notification_url": "http://192.168.1.10:8080/notify",
  "subscriptions": ["order-service"]
}
```

#### Unregister Service
```
DELETE /unregister?service_name=user-service&pod_name=user-service-pod-1
```

#### Get All Services (Debug)
```
GET /services
```

#### Health Check
```
GET /health
```

### Notification Payload

Services receive notifications at their `notification_url`:

```json
{
  "service_name": "order-service",
  "event_type": "register|unregister|update|reconcile",
  "timestamp": "2025-12-14T10:00:00Z",
  "pods": [
    {
      "pod_name": "order-service-pod-1",
      "status": "healthy|unhealthy|unknown",
      "providers": [
        {
          "protocol": "http",
          "ip": "192.168.1.20",
          "port": 8080
        }
      ]
    }
  ]
}
```

## Event Processing

The library uses a single event queue with one worker for sequential processing:

1. **Register Event** (with deadline) - Service registration
2. **Unregister Event** (with deadline) - Service unregistration
3. **Health Check Event** (no deadline) - Periodic health verification
4. **Reconcile Event** (no deadline) - Full state synchronization

Events are processed in FIFO order. Register/Unregister events have deadlines for priority handling, while health check and reconcile events run in the background without deadlines.

## Configuration

### ManagerConfig

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| ServerPort | int | 8080 | HTTP server port |
| HealthCheckInterval | time.Duration | 30s | How often to check service health |
| HealthCheckTimeout | time.Duration | 5s | Timeout for health check HTTP calls |
| HealthCheckRetry | int | 3 | Number of retries before marking unhealthy |
| NotificationInterval | time.Duration | 60s | Periodic reconciliation interval |
| NotificationTimeout | time.Duration | 5s | Timeout for notification HTTP calls |
| EventQueueSize | int | 1000 | Event queue buffer size |

## Supported Protocols

- HTTP
- TCP
- PFCP
- GTP
- UDP

## Examples

See the [examples](./examples) directory for complete working examples:

- [Manager Example](./examples/manager_example/main.go) - Running the governance manager
- [Service Example](./examples/service_example/main.go) - Registering a service

## License

MIT License