package deploys

import (
	"context"
	"strings"

	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func ResolveDeployTemplate(ctx context.Context, service bson.M) (bson.M, error) {
	if id := shared.StringValue(service["deployTemplateId"]); id != "" {
		return shared.FindOne(ctx, shared.Collection(shared.DeployTemplatesCollection), bson.M{"id": id})
	}
	sourceType := normalizeDeploySourceType(shared.StringValue(service["sourceType"]))
	if sourceType == "" {
		if shared.StringValue(service["repoUrl"]) != "" {
			sourceType = "git"
		} else if shared.StringValue(service["dockerImage"]) != "" {
			sourceType = "registry"
		}
	}
	if sourceType != "" {
		return shared.FindOne(ctx, shared.Collection(shared.DeployTemplatesCollection), bson.M{"type": sourceType})
	}
	return bson.M{}, nil
}

func normalizeDeploySourceType(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "registry", "docker":
		return "registry"
	case "git":
		return "git"
	default:
		return ""
	}
}

func ResolveSecretProvider(ctx context.Context, service bson.M) (bson.M, error) {
	settings, err := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{})
	if err != nil {
		return bson.M{}, err
	}
	secrets, _ := settings["secrets"].(bson.M)
	defaultID := shared.StringValue(secrets["defaultProviderId"])
	providerID := shared.StringValue(service["secretProviderId"])
	if providerID == "" {
		providerID = defaultID
	}
	if providerID == "" {
		return bson.M{}, nil
	}
	rawProviders, _ := secrets["providers"].([]interface{})
	for _, item := range rawProviders {
		switch provider := item.(type) {
		case bson.M:
			if shared.StringValue(provider["id"]) == providerID {
				return provider, nil
			}
		case map[string]interface{}:
			if shared.StringValue(provider["id"]) == providerID {
				return bson.M(provider), nil
			}
		}
	}
	return bson.M{}, nil
}
