package mongostore

import (
	"context"
	"crypto/tls"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"releaseaapi/internal/platform/config"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	mongoClient *mongo.Client
	once        sync.Once
)

// DBName is the default database name for Releasea API
const DBName = "releasea"

// Mongo returns a singleton MongoDB client, initialized from config
func Mongo() *mongo.Client {
	once.Do(func() {
		cfg := config.LoadConfig()
		if strings.TrimSpace(cfg.MongoURI) == "" {
			log.Fatal("failed to connect to MongoDB: MONGO_URI is empty")
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		clientOptions := options.Client().ApplyURI(cfg.MongoURI)
		if isTLSSkipVerifyEnabled() {
			clientOptions.SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
		}
		client, err := mongo.Connect(ctx, clientOptions)
		if err != nil {
			log.Fatalf("failed to connect to MongoDB: %v", err)
		}
		if err = client.Ping(ctx, nil); err != nil {
			log.Fatalf("failed to ping MongoDB: %v", err)
		}
		mongoClient = client
	})
	return mongoClient
}

func isTLSSkipVerifyEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("MONGO_TLS_INSECURE")))
	return value == "true" || value == "1" || value == "yes"
}
