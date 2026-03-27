package deploys

import (
	"context"
	"errors"
	"strings"

	secretsproviders "releaseaapi/internal/platform/providers/secrets"
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func ResolveDeployTemplate(ctx context.Context, service bson.M) (bson.M, error) {
	templateID := normalizeTemplateID(shared.StringValue(service["deployTemplateId"]))
	templateType := resolveTemplateType(service, templateID)

	if templateID != "" {
		template, err := shared.FindOne(ctx, shared.Collection(shared.DeployTemplatesCollection), bson.M{"id": templateID})
		if err == nil {
			return normalizeTemplateDocument(template, templateID, templateType), nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return bson.M{}, err
		}
	}

	if templateType != "" {
		template, err := shared.FindOne(ctx, shared.Collection(shared.DeployTemplatesCollection), bson.M{"type": templateType})
		if err == nil {
			return normalizeTemplateDocument(template, templateID, templateType), nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return bson.M{}, err
		}
	}

	fallbackID := templateID
	if fallbackID == "" && templateType != "" {
		switch templateType {
		case "cronjob":
			fallbackID = "tpl-cronjob"
		case "registry":
			fallbackID = "tpl-registry"
		default:
			fallbackID = "tpl-git"
		}
	}
	defaultResources := DefaultDeployTemplateResources(fallbackID, templateType)
	if len(defaultResources) == 0 {
		return bson.M{}, nil
	}
	return bson.M{
		"id":        fallbackID,
		"type":      templateType,
		"name":      fallbackID,
		"resources": defaultResources,
	}, nil
}

func normalizeDeploySourceType(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "registry", "docker":
		return "registry"
	case "git":
		return "git"
	case "cronjob", "scheduled-job":
		return "cronjob"
	default:
		return ""
	}
}

func normalizeTemplateID(templateID string) string {
	return strings.ToLower(strings.TrimSpace(templateID))
}

func resolveTemplateType(service bson.M, templateID string) string {
	switch templateID {
	case "tpl-cronjob":
		return "cronjob"
	case "tpl-registry":
		return "registry"
	case "tpl-git":
		return "git"
	}

	sourceType := normalizeDeploySourceType(shared.StringValue(service["sourceType"]))
	if sourceType != "" {
		return sourceType
	}
	if shared.StringValue(service["scheduleCron"]) != "" || shared.StringValue(service["scheduleCommand"]) != "" {
		return "cronjob"
	}
	if shared.StringValue(service["repoUrl"]) != "" {
		return "git"
	}
	if shared.StringValue(service["dockerImage"]) != "" {
		return "registry"
	}
	return ""
}

func normalizeTemplateDocument(template bson.M, templateID, templateType string) bson.M {
	if template == nil {
		template = bson.M{}
	}
	if shared.StringValue(template["id"]) == "" && templateID != "" {
		template["id"] = templateID
	}
	if shared.StringValue(template["type"]) == "" && templateType != "" {
		template["type"] = templateType
	}
	if _, ok := template["resources"]; !ok || template["resources"] == nil {
		template["resources"] = DefaultDeployTemplateResources(shared.StringValue(template["id"]), shared.StringValue(template["type"]))
	}
	return template
}

func ResolveSecretProvider(ctx context.Context, service bson.M) (bson.M, error) {
	settings, err := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{})
	if err != nil {
		return bson.M{}, err
	}
	return secretsproviders.ResolveConfiguredProviderDocument(settings, shared.StringValue(service["secretProviderId"])), nil
}
