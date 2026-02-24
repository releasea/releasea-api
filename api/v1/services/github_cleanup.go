package services

import (
	"context"
	"strings"

	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func resolveServiceScmCredential(ctx context.Context, service bson.M, project bson.M) (bson.M, error) {
	if id := strings.TrimSpace(shared.StringValue(service["scmCredentialId"])); id != "" {
		return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
	}
	if project != nil {
		if id := strings.TrimSpace(shared.StringValue(project["scmCredentialId"])); id != "" {
			return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
		}
	}
	return shared.FindLatestPlatformCredential(ctx, shared.ScmCredentialsCollection)
}
