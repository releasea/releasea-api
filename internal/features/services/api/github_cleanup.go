package services

import (
	"context"
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func resolveServiceScmCredential(ctx context.Context, service bson.M, project bson.M) (bson.M, error) {
	var (
		credential bson.M
		err        error
	)
	if id := strings.TrimSpace(shared.StringValue(service["scmCredentialId"])); id != "" {
		credential, err = shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
	} else if project != nil {
		if id := strings.TrimSpace(shared.StringValue(project["scmCredentialId"])); id != "" {
			credential, err = shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
		}
	}
	if credential == nil && err == nil {
		credential, err = shared.FindLatestPlatformCredential(ctx, shared.ScmCredentialsCollection)
	}
	if err != nil {
		return nil, err
	}
	return shared.DecryptCredentialDocument(credential, "token", "privateKey")
}
