package mongostore

import (
	"context"
	"crypto/tls"
	"fmt"
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
	connectErr  error
	once        sync.Once
)

// DBName is the default database name for Releasea API
const DBName = "releasea"

// Connect initializes and verifies the singleton MongoDB client.
func Connect(ctx context.Context) error {
	once.Do(func() {
		cfg := config.LoadConfig()
		if strings.TrimSpace(cfg.MongoURI) == "" {
			connectErr = fmt.Errorf("MONGO_URI is empty")
			return
		}

		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		clientOptions := options.Client().ApplyURI(cfg.MongoURI)
		if isTLSSkipVerifyEnabled() {
			clientOptions.SetTLSConfig(&tls.Config{InsecureSkipVerify: true})
		}
		client, err := mongo.Connect(ctx, clientOptions)
		if err != nil {
			connectErr = fmt.Errorf("connect to MongoDB: %w", err)
			return
		}
		if err = client.Ping(ctx, nil); err != nil {
			_ = client.Disconnect(context.Background())
			connectErr = fmt.Errorf("ping MongoDB: %w", err)
			return
		}
		mongoClient = client
	})
	return connectErr
}

// Mongo returns the initialized singleton. Applications should call Connect
// during startup so connection failures can be reported cleanly.
func Mongo() *mongo.Client {
	if err := Connect(context.Background()); err != nil {
		panic(fmt.Sprintf("MongoDB client is unavailable: %v", err))
	}
	return mongoClient
}

func Disconnect(ctx context.Context) error {
	if mongoClient == nil {
		return nil
	}
	return mongoClient.Disconnect(ctx)
}

func isTLSSkipVerifyEnabled() bool {
	value := strings.ToLower(strings.TrimSpace(os.Getenv("MONGO_TLS_INSECURE")))
	return value == "true" || value == "1" || value == "yes"
}
