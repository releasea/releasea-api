package main

import (
	"log"
	"os"
	"releaseaapi/internal/platform/bootstrap"
	"releaseaapi/internal/platform/config"
	"releaseaapi/internal/platform/http/router"
	mongostore "releaseaapi/internal/platform/storage/mongo"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func loadEnvFiles() {
	files := []string{".env", ".env.local", ".env.local.cluster"}
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

	// Initialize Mongo (fatal if it fails)
	mongostore.Mongo()

	// Ensure required initial data
	bootstrap.Setup(cfg)

	// Start the Gin API
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("failed to set trusted proxies: %v", err)
	}
	r.Use(gin.Logger(), gin.Recovery())
	router.SetupRoutes(r)
	r.Run(":" + cfg.Port)
}
