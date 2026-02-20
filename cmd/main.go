package main

import (
	"log"
	"releaseaapi/api/v1"
	"releaseaapi/client"
	"releaseaapi/config"
	"releaseaapi/services"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	cfg := config.LoadConfig()

	// Initialize Mongo (fatal if it fails)
	client.Mongo()

	// Ensure required initial data
	services.Setup(cfg)

	// Start the Gin API
	r := gin.New()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("failed to set trusted proxies: %v", err)
	}
	r.Use(gin.Logger(), gin.Recovery())
	v1.SetupRoutes(r)
	r.Run(":" + cfg.Port)
}
