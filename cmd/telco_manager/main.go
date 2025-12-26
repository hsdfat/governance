package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/chronnie/governance/internal/config"
	"github.com/chronnie/governance/manager"
	"github.com/chronnie/governance/models"
	"github.com/chronnie/governance/storage/postgres"
)

func main() {
	log.Println("Starting Telco Governance Manager")
	log.Println("Managing lifecycle for: http-gw, diam-gw, and eir services")

	// Load configuration
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Database configuration
	dbConfig := postgres.Config{
		Host:            cfg.Database.Host,
		Port:            cfg.Database.Port,
		Database:        cfg.Database.Database,
		Username:        cfg.Database.Username,
		Password:        cfg.Database.Password,
		SSLMode:         cfg.Database.SSLMode,
		MaxOpenConns:    cfg.Database.MaxOpenConns,
		MaxIdleConns:    cfg.Database.MaxIdleConns,
		ConnMaxLifetime: cfg.Database.ConnMaxLifetime,
	}

	// Create PostgreSQL database store
	db, err := postgres.NewDatabaseStore(dbConfig)
	if err != nil {
		log.Fatalf("Failed to create database store: %v", err)
	}

	// Manager configuration
	managerConfig := &models.ManagerConfig{
		ServerPort:           cfg.Server.Port,
		HealthCheckInterval:  cfg.Manager.HealthCheckInterval,
		HealthCheckTimeout:   cfg.Manager.HealthCheckTimeout,
		HealthCheckRetry:     cfg.Manager.HealthCheckRetry,
		NotificationInterval: cfg.Manager.NotificationInterval,
		NotificationTimeout:  cfg.Manager.NotificationTimeout,
		EventQueueSize:       cfg.Manager.EventQueueSize,
	}

	// Create and start manager with database persistence
	mgr := manager.NewManagerWithDatabase(managerConfig, db)
	if err := mgr.Start(); err != nil {
		log.Fatalf("Failed to start manager: %v", err)
	}

	log.Println("\n========================================")
	log.Println("Telco Governance Manager Started!")
	log.Println("========================================")

	log.Println("\nAPI Endpoints:")
	log.Printf("  POST   http://localhost:%d/register\n", cfg.Server.Port)
	log.Printf("  DELETE http://localhost:%d/unregister\n", cfg.Server.Port)
	log.Printf("  GET    http://localhost:%d/services\n", cfg.Server.Port)
	log.Printf("  GET    http://localhost:%d/health\n", cfg.Server.Port)

	log.Println("\nConfiguration:")
	log.Printf("  Database: %s@%s:%d/%s\n", cfg.Database.Username, cfg.Database.Host, cfg.Database.Port, cfg.Database.Database)
	log.Printf("  Health Check: interval=%s timeout=%s retry=%d\n", cfg.Manager.HealthCheckInterval, cfg.Manager.HealthCheckTimeout, cfg.Manager.HealthCheckRetry)
	log.Printf("  Notification: interval=%s timeout=%s\n", cfg.Manager.NotificationInterval, cfg.Manager.NotificationTimeout)
	log.Printf("  Logging: level=%s format=%s\n", cfg.Logging.Level, cfg.Logging.Format)

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
