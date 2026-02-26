package main

import (
	"log"
	"releaseaapi/internal/platform/bootstrap"
	"releaseaapi/internal/platform/config"
	"releaseaapi/internal/platform/http/router"
	mongostore "releaseaapi/internal/platform/storage/mongo"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
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
