# In-Memory Storage Implementations

This package provides two in-memory storage implementations for the governance system's registry store.

## Implementations

### 1. MemoryStore (Lock-Free)

**File:** `memory.go`

A lock-free in-memory storage implementation optimized for single-threaded access.

**Use Cases:**
- Default storage for the governance manager
- Single event queue worker scenarios
- Low-latency requirements
- Simplicity and performance

**Characteristics:**
- ✅ No locking overhead
- ✅ Fastest performance
- ⚠️ **NOT thread-safe** - must be accessed from a single goroutine
- ✅ Suitable for event queue worker pattern

**Example:**
```go
store := memory.NewMemoryStore()
registry := registry.NewRegistry(store)
```

### 2. ThreadSafeMemoryStore (Thread-Safe)

**File:** `threadsafe_memory.go`

A thread-safe in-memory storage implementation with RWMutex protection.

**Use Cases:**
- Testing with concurrent access
- Multi-threaded applications
- Client-side caching
- Development and debugging

**Characteristics:**
- ✅ Thread-safe with RWMutex
- ✅ Concurrent read operations
- ✅ Deep copy protection
- ✅ Additional utility methods
- ⚠️ Slightly slower due to locking

**Example:**
```go
store := memory.NewThreadSafeMemoryStore()

// Safe for concurrent use
go func() {
    store.SaveService(ctx, service1)
}()
go func() {
    services, _ := store.GetAllServices(ctx)
}()
```

## Interface Compliance

Both implementations fully comply with the `storage.RegistryStore` interface:

### Service Operations
- `SaveService` - Store or update a service
- `GetService` - Retrieve a single service by key
- `GetServicesByName` - Get all pods for a service
- `GetAllServices` - Get all registered services
- `DeleteService` - Remove a service
- `UpdateHealthStatus` - Update health status

### Subscription Operations
- `AddSubscription` - Add a subscriber to a service group
- `RemoveSubscription` - Remove a subscriber
- `RemoveAllSubscriptions` - Remove all subscriptions for a subscriber
- `GetSubscribers` - Get all subscribers for a service
- `GetSubscriberServices` - Get full ServiceInfo for subscribers

### Lifecycle Operations
- `Close` - Cleanup resources (no-op for memory stores)
- `Ping` - Health check (always succeeds for memory stores)

## Additional Features (ThreadSafeMemoryStore Only)

### Clear
Remove all data from the store (useful for testing):
```go
store.Clear()
```

### GetStats
Get statistics about the store:
```go
serviceCount, subscriptionCount := store.GetStats()
fmt.Printf("Services: %d, Subscriptions: %d\n", serviceCount, subscriptionCount)
```

## Data Protection

Both implementations provide protection against external mutations:

### MemoryStore
- Returns shallow copies of services
- Copies slice references

### ThreadSafeMemoryStore
- Returns **deep copies** of services
- Copies all nested data structures
- Protects against concurrent modifications

## Performance Comparison

| Operation | MemoryStore | ThreadSafeMemoryStore |
|-----------|-------------|----------------------|
| SaveService | ~100ns | ~150ns |
| GetService | ~80ns | ~120ns |
| GetAllServices | ~500ns | ~800ns |
| Concurrent Safety | ❌ | ✅ |

*Note: Benchmarks are approximate and depend on data size*

## Testing

Both implementations have comprehensive test coverage:

```bash
# Test MemoryStore
go test ./storage/memory -run Memory -v

# Test ThreadSafeMemoryStore
go test ./storage/memory -run ThreadSafe -v

# Test with race detector
go test ./storage/memory -race
```

### Test Coverage

#### MemoryStore
- Basic CRUD operations
- Subscription management
- Edge cases (nil, not found, etc.)

#### ThreadSafeMemoryStore (19 tests)
- All MemoryStore tests
- Deep copy verification
- Concurrent access safety (3000 operations across 30 goroutines)
- Additional utility methods

## Choosing the Right Implementation

### Use MemoryStore When:
- ✅ Single-threaded access (event queue worker)
- ✅ Maximum performance is critical
- ✅ Simple deployment
- ✅ Default governance manager setup

### Use ThreadSafeMemoryStore When:
- ✅ Multiple goroutines access the store
- ✅ Testing scenarios
- ✅ Client-side caching
- ✅ Need utility methods (Clear, GetStats)
- ✅ Development and debugging

## Migration Between Implementations

Both stores implement the same interface, making migration seamless:

```go
// Before (lock-free)
store := memory.NewMemoryStore()

// After (thread-safe)
store := memory.NewThreadSafeMemoryStore()

// No other code changes needed
registry := registry.NewRegistry(store)
```

## Thread Safety Guarantees

### ThreadSafeMemoryStore Guarantees:
1. ✅ All operations are atomic
2. ✅ Concurrent reads are safe
3. ✅ Read-write conflicts are prevented
4. ✅ Data races are eliminated
5. ✅ Returned data is immutable (deep copies)

### Verified by:
- ✅ Race detector tests
- ✅ Concurrent access tests (3000 operations)
- ✅ Production usage patterns

## Examples

### Basic Usage
```go
package main

import (
    "context"
    "github.com/chronnie/governance/models"
    "github.com/chronnie/governance/storage/memory"
)

func main() {
    // Create thread-safe store
    store := memory.NewThreadSafeMemoryStore()
    ctx := context.Background()

    // Save a service
    service := &models.ServiceInfo{
        ServiceName: "user-service",
        PodName:     "pod-1",
        Status:      models.StatusHealthy,
        // ... other fields
    }
    store.SaveService(ctx, service)

    // Retrieve service
    retrieved, err := store.GetService(ctx, "user-service:pod-1")
    if err != nil {
        panic(err)
    }

    // Add subscription
    store.AddSubscription(ctx, "subscriber:pod-1", "user-service")

    // Get stats
    services, subscriptions := store.GetStats()
    println("Services:", services, "Subscriptions:", subscriptions)
}
```

### Testing Usage
```go
func TestMyFeature(t *testing.T) {
    store := memory.NewThreadSafeMemoryStore()
    defer store.Clear() // Clean up after test

    // Your test code here
    // ...

    // Verify state
    count, _ := store.GetStats()
    if count != expectedCount {
        t.Errorf("Expected %d services, got %d", expectedCount, count)
    }
}
```

## License

Part of the governance package.
