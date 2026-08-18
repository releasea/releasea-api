package bootstrap

import (
	"context"
	"fmt"
	"time"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type collectionIndexes struct {
	collection string
	indexes    []mongo.IndexModel
}

// EnsureIndexes installs the uniqueness, lookup, and expiry indexes required
// by the API's hot paths. MongoDB treats this operation as idempotent.
func EnsureIndexes(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	definitions := []collectionIndexes{
		{shared.UsersCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetName("users_email_unique")},
		}},
		{shared.ServicesCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("services_id_unique")},
			{Keys: bson.D{{Key: "projectId", Value: 1}}, Options: options.Index().SetName("services_project")},
		}},
		{shared.ProjectsCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("projects_id_unique")},
		}},
		{shared.RulesCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("rules_id_unique")},
			{Keys: bson.D{{Key: "serviceId", Value: 1}}, Options: options.Index().SetName("rules_service")},
		}},
		{shared.DeploysCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("deploys_id_unique")},
			{Keys: bson.D{{Key: "serviceId", Value: 1}, {Key: "startedAt", Value: -1}}, Options: options.Index().SetName("deploys_service_started")},
		}},
		{shared.OperationsCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("operations_id_unique")},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "createdAt", Value: 1}}, Options: options.Index().SetName("operations_status_created")},
			{Keys: bson.D{{Key: "claim.expiresAt", Value: 1}}, Options: options.Index().SetName("operations_claim_expiry")},
		}},
		{shared.WorkersCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("workers_id_unique")},
			{Keys: bson.D{{Key: "lastHeartbeat", Value: -1}}, Options: options.Index().SetName("workers_heartbeat")},
		}},
		{shared.PasswordResetsCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "tokenHash", Value: 1}}, Options: options.Index().SetUnique(true).SetName("password_resets_token_unique")},
			{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0).SetName("password_resets_expiry")},
		}},
		{shared.IdempotencyKeysCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0).SetName("idempotency_keys_expiry")},
		}},
		{shared.ScmCredentialsCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("scm_credentials_id_unique")},
			{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "projectId", Value: 1}, {Key: "serviceId", Value: 1}}, Options: options.Index().SetName("scm_credentials_scope")},
		}},
		{shared.RegistryCredentialsCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("registry_credentials_id_unique")},
			{Keys: bson.D{{Key: "scope", Value: 1}, {Key: "projectId", Value: 1}, {Key: "serviceId", Value: 1}}, Options: options.Index().SetName("registry_credentials_scope")},
		}},
		{shared.PlatformAuditCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "resourceType", Value: 1}, {Key: "resourceId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("platform_audit_resource_created")},
		}},
		{shared.AIProvidersCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("ai_providers_id_unique")},
			{Keys: bson.D{{Key: "default", Value: -1}, {Key: "updatedAt", Value: -1}}, Options: options.Index().SetName("ai_providers_default_updated")},
		}},
		{shared.AIAnalysesCollection, []mongo.IndexModel{
			{Keys: bson.D{{Key: "serviceId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("ai_analyses_service_created")},
			{Keys: bson.D{{Key: "providerId", Value: 1}, {Key: "createdAt", Value: -1}}, Options: options.Index().SetName("ai_analyses_provider_created")},
			{Keys: bson.D{{Key: "expiresAt", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0).SetName("ai_analyses_expiry")},
		}},
	}

	for _, definition := range definitions {
		if _, err := shared.Collection(definition.collection).Indexes().CreateMany(ctx, definition.indexes); err != nil {
			return fmt.Errorf("create indexes for %s: %w", definition.collection, err)
		}
	}
	return nil
}
