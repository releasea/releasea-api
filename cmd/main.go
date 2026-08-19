package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"releaseaapi/internal/platform/bootstrap"
	"releaseaapi/internal/platform/config"
	"releaseaapi/internal/platform/http/router"
	mongostore "releaseaapi/internal/platform/storage/mongo"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func loadEnvFiles() {
	files := []string{".env", ".env.local", ".env.local.cluster", ".env.local.compose"}
	merged := map[string]string{}
	for _, file := range files {
		values, err := godotenv.Read(file)
		if err != nil {
			continue
		}
		for key, value := range values {
			merged[key] = value
		}
	}
	for key, value := range merged {
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func main() {
	loadEnvFiles()
	cfg := config.LoadConfig()
	if err := config.ValidateProductionSecurity(cfg); err != nil {
		log.Fatalf("invalid production configuration: %v", err)
	}

	startupContext, startupCancel := context.WithTimeout(context.Background(), 15*time.Second)
	if err := mongostore.Connect(startupContext); err != nil {
		startupCancel()
		log.Fatalf("failed to initialize MongoDB: %v", err)
	}
	startupCancel()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mongostore.Disconnect(ctx); err != nil {
			log.Printf("failed to disconnect MongoDB: %v", err)
		}
	}()

	// Ensure required initial data
	bootstrap.Setup(cfg)
	indexContext, indexCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer indexCancel()
	if err := bootstrap.EnsureIndexes(indexContext); err != nil {
		log.Fatalf("failed to ensure database indexes: %v", err)
	}

	// Start the Gin API
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("failed to set trusted proxies: %v", err)
	}
	r.Use(gin.Logger(), gin.Recovery())
	router.SetupRoutes(r)
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	shutdownContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("API server failed: %v", err)
		}
	case <-shutdownContext.Done():
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("API server shutdown failed: %v", err)
		}
	}
}
