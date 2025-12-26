package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chronnie/governance/manager"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage/postgres"
)

func main() {
	log.Println("Starting Telco Governance Manager")
	log.Println("Managing lifecycle for: http-gw, diam-gw, and eir services")

	// Database configuration from environment
	dbConfig := postgres.Config{
		Host:            getEnv("DB_HOST", "localhost"),
		Port:            getEnvInt("DB_PORT", 5432),
		Database:        getEnv("DB_NAME", "governance_db"),
		Username:        getEnv("DB_USER", "postgres"),
		Password:        getEnv("DB_PASSWORD", "postgres"),
		SSLMode:         getEnv("DB_SSL_MODE", "disable"),
		MaxOpenConns:    25,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
	}

	// Create PostgreSQL database store
	db, err := postgres.NewDatabaseStore(dbConfig)
	if err != nil {
		log.Fatalf("Failed to create database store: %v", err)
	}

	// Manager configuration
	config := &models.ManagerConfig{
		ServerPort:           getEnvInt("SERVER_PORT", 8080),
		HealthCheckInterval:  getDurationEnv("HEALTH_CHECK_INTERVAL", 30*time.Second),
		HealthCheckTimeout:   getDurationEnv("HEALTH_CHECK_TIMEOUT", 5*time.Second),
		HealthCheckRetry:     getEnvInt("HEALTH_CHECK_RETRY", 3),
		NotificationInterval: getDurationEnv("NOTIFICATION_INTERVAL", 60*time.Second),
		NotificationTimeout:  getDurationEnv("NOTIFICATION_TIMEOUT", 5*time.Second),
		EventQueueSize:       getEnvInt("EVENT_QUEUE_SIZE", 1000),
	}

	// Create and start manager with database persistence
	mgr := manager.NewManagerWithDatabase(config, db)
	if err := mgr.Start(); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}

	log.Println("\n========================================")
	log.Println("Telco Governance Manager Started!")
	log.Println("========================================")

	log.Println("\nAPI Endpoints:")
	log.Printf("  POST   http://localhost:%d/register\n", config.ServerPort)
	log.Printf("  DELETE http://localhost:%d/unregister\n", config.ServerPort)
	log.Printf("  GET    http://localhost:%d/services\n", config.ServerPort)
	log.Printf("  GET    http://localhost:%d/health\n", config.ServerPort)

	log.Println("\nPress Ctrl+C to stop...")

	// Wait for interrupt signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	<-sigChan

	log.Println("\nShutting down...")
	if err := mgr.Stop(); err != nil {
		log.Fatalf("Failed to stop: %v", err)
	}

	log.Println("Stopped successfully")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getDurationEnv(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}
