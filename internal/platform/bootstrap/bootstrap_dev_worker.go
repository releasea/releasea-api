package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"releaseaapi/internal/platform/shared"
	mongostore "releaseaapi/internal/platform/storage/mongo"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
	"golang.org/x/crypto/bcrypt"
)

func ensureBootstrapDevWorkerRegistration() error {
	if !shared.EnvBool("RELEASEA_BOOTSTRAP_DEV_WORKER_ENABLED", false) {
		return nil
	}

	tokenValue := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_TOKEN", ""))
	if tokenValue == "" {
		return fmt.Errorf("RELEASEA_BOOTSTRAP_DEV_WORKER_TOKEN is required when RELEASEA_BOOTSTRAP_DEV_WORKER_ENABLED=true")
	}

	registrationID := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_ID", "wkr-reg-bootstrap-dev"))
	if registrationID == "" {
		registrationID = "wkr-reg-bootstrap-dev"
	}

	environment := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_ENVIRONMENT", "dev"))
	if environment == "" {
		environment = "dev"
	}

	platformNamespace := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_SYSTEM_NAMESPACE", "releasea-system"))
	if platformNamespace == "" {
		platformNamespace = "releasea-system"
	}

	namespace := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_NAMESPACE", platformNamespace))
	if namespace == "" {
		namespace = platformNamespace
	}

	namespacePrefix := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_NAMESPACE_PREFIX", "releasea-apps"))
	if namespacePrefix == "" {
		namespacePrefix = "releasea-apps"
	}

	name := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_NAME", "Development Worker"))
	if name == "" {
		name = "Development Worker"
	}

	cluster := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_CLUSTER", "quickstart-local"))
	if cluster == "" {
		cluster = "quickstart-local"
	}

	notes := strings.TrimSpace(shared.EnvOrDefault(
		"RELEASEA_BOOTSTRAP_DEV_WORKER_NOTES",
		"Managed bootstrap worker for quickstart and local installs.",
	))

	tags := parseBootstrapDevWorkerTags(shared.EnvOrDefault("RELEASEA_BOOTSTRAP_DEV_WORKER_TAGS", "dev,build"), environment)

	hashedToken, err := bcrypt.GenerateFromPassword([]byte(tokenValue), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap worker token: %w", err)
	}

	now := shared.NowISO()
	ctx, cancel := context.WithTimeout(context.Background(), dbTimeout)
	defer cancel()

	db := mongostore.Mongo().Database(mongostore.DBName)
	_, err = db.Collection(shared.WorkerRegistrationsCollection).UpdateOne(
		ctx,
		bson.M{"_id": registrationID},
		bson.M{
			"$set": bson.M{
				"id":              registrationID,
				"name":            name,
				"environment":     environment,
				"tags":            tags,
				"cluster":         cluster,
				"namespacePrefix": namespacePrefix,
				"namespace":       namespace,
				"notes":           notes,
				"managed":         true,
				"managedBy":       "platform-bootstrap",
				"tokenHash":       string(hashedToken),
				"tokenHint":       shared.TokenHint(tokenValue),
				"updatedAt":       now,
			},
			"$setOnInsert": bson.M{
				"_id":       registrationID,
				"createdAt": now,
				"status":    "unused",
			},
		},
		options.Update().SetUpsert(true),
	)
	if err != nil {
		return fmt.Errorf("upsert bootstrap dev worker registration: %w", err)
	}

	return nil
}

func parseBootstrapDevWorkerTags(raw string, environment string) []string {
	parts := strings.Split(raw, ",")
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		tags = append(tags, trimmed)
	}
	if len(tags) == 0 && strings.TrimSpace(environment) != "" {
		tags = append(tags, strings.TrimSpace(environment))
	}
	return shared.UniqueStrings(tags)
}
