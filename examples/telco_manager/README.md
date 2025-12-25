# Telco Governance Manager

A governance manager for managing the lifecycle of telco services (http-gw, diam-gw, and eir). This example uses the governance library with PostgreSQL persistence to provide service registration, discovery, health monitoring, and lifecycle management.

## Features

- **Service Registration**: Register http-gw, diam-gw, and eir services
- **Service Discovery**: Services can subscribe to other services for updates
- **Health Monitoring**: Automatic periodic health checks with retries
- **State Persistence**: All service state stored in PostgreSQL
- **Event-Driven**: Lock-free event queue for processing
- **Lifecycle Management**: Register and unregister services manages their lifecycle

## How It Works

The governance manager tracks the lifecycle through registration and health status:

1. **Service Registers**: Service calls `/register` endpoint
   - Service enters the system as `healthy`/`unhealthy`/`unknown` status
   - Stored in PostgreSQL `services` table

2. **Health Monitoring**: Manager periodically checks service health
   - HTTP GET to `health_check_url`
   - Updates service status based on response
   - Retries on failures (configurable)

3. **Service Subscriptions**: Services can subscribe to other services
   - Receive notifications when subscribed services change
   - Notifications sent to `notification_url`

4. **Service Unregisters**: Service calls `/unregister` endpoint
   - Service removed from registry
   - Deleted from PostgreSQL

## Running

### 1. Start PostgreSQL

```bash
docker run --name postgres \
  -e POSTGRES_DB=governance_db \
  -e POSTGRES_USER=postgres \
  -e POSTGRES_PASSWORD=postgres \
  -p 5432:5432 \
  -d postgres:15-alpine
```

### 2. Run the Manager

```bash
cd examples/telco_manager
go run main.go
```

Or with custom configuration:

```bash
DB_HOST=localhost \
DB_PORT=5432 \
DB_NAME=governance_db \
DB_USER=postgres \
DB_PASSWORD=postgres \
SERVER_PORT=8080 \
HEALTH_CHECK_INTERVAL=30s \
NOTIFICATION_INTERVAL=60s \
go run main.go
```

## API Examples

### Register HTTP-GW

```bash
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{
    "service_name": "http-gw",
    "pod_name": "http-gw-pod-1",
    "providers": [
      {
        "protocol": "http",
        "ip": "192.168.1.10",
        "port": 8080
      }
    ],
    "health_check_url": "http://192.168.1.10:8080/health",
    "notification_url": "http://192.168.1.10:8080/notify",
    "subscriptions": ["diam-gw", "eir"]
  }'
```

### Register Diam-GW

```bash
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{
    "service_name": "diam-gw",
    "pod_name": "diam-gw-pod-1",
    "providers": [
      {
        "protocol": "tcp",
        "ip": "192.168.1.20",
        "port": 3868
      }
    ],
    "health_check_url": "http://192.168.1.20:8080/health",
    "notification_url": "http://192.168.1.20:8080/notify",
    "subscriptions": ["eir"]
  }'
```

### Register EIR

```bash
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{
    "service_name": "eir",
    "pod_name": "eir-pod-1",
    "providers": [
      {
        "protocol": "http",
        "ip": "192.168.1.30",
        "port": 8080
      },
      {
        "protocol": "tcp",
        "ip": "192.168.1.30",
        "port": 3868
      }
    ],
    "health_check_url": "http://192.168.1.30:8080/health",
    "notification_url": "http://192.168.1.30:8080/notify",
    "subscriptions": []
  }'
```

### List All Services

```bash
curl http://localhost:8080/services
```

### Unregister a Service

```bash
curl -X DELETE "http://localhost:8080/unregister?service_name=http-gw&pod_name=http-gw-pod-1"
```

### Health Check

```bash
curl http://localhost:8080/health
```

## Configuration

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `DB_HOST` | localhost | PostgreSQL host |
| `DB_PORT` | 5432 | PostgreSQL port |
| `DB_NAME` | governance_db | Database name |
| `DB_USER` | postgres | Database user |
| `DB_PASSWORD` | postgres | Database password |
| `DB_SSL_MODE` | disable | SSL mode (disable, require, verify-ca, verify-full) |
| `SERVER_PORT` | 8080 | HTTP API port |
| `HEALTH_CHECK_INTERVAL` | 30s | How often to check service health |
| `HEALTH_CHECK_TIMEOUT` | 5s | Timeout for health check requests |
| `HEALTH_CHECK_RETRY` | 3 | Number of retries before marking unhealthy |
| `NOTIFICATION_INTERVAL` | 60s | How often to send reconciliation notifications |
| `NOTIFICATION_TIMEOUT` | 5s | Timeout for notification requests |
| `EVENT_QUEUE_SIZE` | 1000 | Event queue buffer size |

## Database Schema

The manager creates a `services` table:

```sql
CREATE TABLE services (
    service_key VARCHAR(255) PRIMARY KEY,  -- "service_name:pod_name"
    service_name VARCHAR(128) NOT NULL,
    pod_name VARCHAR(128) NOT NULL,
    providers JSONB NOT NULL,               -- Array of provider endpoints
    health_check_url VARCHAR(512) NOT NULL,
    notification_url VARCHAR(512) NOT NULL,
    subscriptions JSONB NOT NULL,           -- Array of subscribed service names
    status VARCHAR(20) NOT NULL,            -- healthy, unhealthy, unknown
    last_health_check TIMESTAMP NOT NULL,
    registered_at TIMESTAMP NOT NULL,
    updated_at TIMESTAMP NOT NULL
);
```

### Query Examples

```bash
# View all services
psql -h localhost -U postgres -d governance_db -c "SELECT * FROM services"

# View services by type
psql -h localhost -U postgres -d governance_db -c \
  "SELECT service_name, COUNT(*) FROM services GROUP BY service_name"

# View service health status
psql -h localhost -U postgres -d governance_db -c \
  "SELECT service_key, status, last_health_check FROM services ORDER BY last_health_check DESC"

# View service subscriptions
psql -h localhost -U postgres -d governance_db -c \
  "SELECT service_key, subscriptions FROM services WHERE jsonb_array_length(subscriptions) > 0"
```

## Integration with Services

### HTTP-GW Integration Example

```go
package main

import (
    "github.com/chronnie/governance/client"
    "github.com/chronnie/governance/models"
)

func main() {
    // Create governance client
    govClient := client.NewClient(&client.ClientConfig{
        ManagerURL:  "http://localhost:8080",
        ServiceName: "http-gw",
        PodName:     "http-gw-pod-1",
    })

    // Register service
    registration := &models.ServiceRegistration{
        ServiceName: "http-gw",
        PodName:     "http-gw-pod-1",
        Providers: []models.ProviderInfo{
            {Protocol: models.ProtocolHTTP, IP: "0.0.0.0", Port: 8080},
        },
        HealthCheckURL:  "http://localhost:8080/health",
        NotificationURL: "http://localhost:8080/notify",
        Subscriptions:   []string{"diam-gw", "eir"},
    }

    if err := govClient.Register(registration); err != nil {
        log.Fatal(err)
    }

    // Start notification server to receive updates
    notifHandler := func(payload *models.NotificationPayload) {
        log.Printf("Received notification: %s changed", payload.ServiceName)
        // Update internal routing/config based on notification
    }

    notifServer := client.NewNotificationServer(8080, notifHandler)
    go notifServer.Start()

    // ... run your service ...

    // Unregister on shutdown
    defer govClient.Unregister()
}
```

## Docker Deployment

See the main `docker-compose.yml` for running the telco governance manager alongside http-gw, diam-gw, and eir services.
