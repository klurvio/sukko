// Package main provides the entry point for the auth-service.
// Auth-service handles JWT token issuance for multi-tenant applications.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/adred-codev/ws_poc/internal/authservice"
)

func main() {
	// Get configuration from environment
	port := getEnv("AUTH_PORT", "3002")
	configPath := getEnv("CONFIG_PATH", "/etc/auth/tenants.yaml")

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET environment variable is required")
	}

	tokenExpiryStr := getEnv("TOKEN_EXPIRY", "24h")
	tokenExpiry, err := time.ParseDuration(tokenExpiryStr)
	if err != nil {
		log.Fatalf("Invalid TOKEN_EXPIRY: %v", err)
	}

	// Load tenant configuration
	cfg, err := authservice.LoadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config from %s: %v", configPath, err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		log.Fatalf("Invalid configuration: %v", err)
	}

	// Create and configure service
	svc := authservice.New(jwtSecret)
	svc.SetConfig(cfg)
	svc.SetTokenExpiry(tokenExpiry)

	// Start HTTP server
	addr := ":" + port
	log.Printf("auth-service starting on %s", addr)
	log.Printf("Loaded %d tenant(s) from config", len(cfg.Tenants))

	server := &http.Server{
		Addr:         addr,
		Handler:      svc.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
