package settings

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	platformmodels "releaseaapi/internal/platform/models"
	identityproviders "releaseaapi/internal/platform/providers/identity"
	notificationproviders "releaseaapi/internal/platform/providers/notifications"
	registryproviders "releaseaapi/internal/platform/providers/registry"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	secretsproviders "releaseaapi/internal/platform/providers/secrets"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	providerHealthHealthy     = "healthy"
	providerHealthUnhealthy   = "unhealthy"
	providerHealthUnsupported = "unsupported"
	providerHealthDisabled    = "disabled"
)

var providerHealthLoader = loadProviderHealthCatalog

func GetProviderHealth(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	health, err := providerHealthLoader(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to run provider health checks")
		return
	}
	c.JSON(http.StatusOK, health)
}

func loadProviderHealthCatalog(ctx context.Context) (platformmodels.ProviderHealthCatalog, error) {
	settings, err := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{})
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return platformmodels.ProviderHealthCatalog{}, err
	}
	if settings == nil {
		settings = bson.M{}
	}

	idpConfig, err := shared.FindOne(ctx, shared.Collection(shared.IdpConfigCollection), bson.M{})
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return platformmodels.ProviderHealthCatalog{}, err
	}
	if idpConfig == nil {
		idpConfig = bson.M{}
	}

	scmCredentials, err := shared.FindAll(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{})
	if err != nil {
		return platformmodels.ProviderHealthCatalog{}, err
	}
	registryCredentials, err := shared.FindAll(ctx, shared.Collection(shared.RegistryCredentialsCollection), bson.M{})
	if err != nil {
		return platformmodels.ProviderHealthCatalog{}, err
	}

	return buildProviderHealthCatalog(ctx, buildProviderCatalog(), settings, idpConfig, scmCredentials, registryCredentials), nil
}

func buildProviderHealthCatalog(
	ctx context.Context,
	catalog platformmodels.ProviderCatalog,
	settings bson.M,
	idpConfig bson.M,
	scmCredentials []bson.M,
	registryCredentials []bson.M,
) platformmodels.ProviderHealthCatalog {
	return platformmodels.ProviderHealthCatalog{
		Version:       catalog.Version,
		SCM:           summarizeHealthChecks(catalog.SCM.Kind, buildSCMHealthChecks(ctx, catalog.SCM, scmCredentials)),
		Registry:      summarizeHealthChecks(catalog.Registry.Kind, buildRegistryHealthChecks(ctx, catalog.Registry, registryCredentials)),
		Secrets:       summarizeHealthChecks(catalog.Secrets.Kind, buildSecretsHealthChecks(ctx, catalog.Secrets, settings)),
		Identity:      summarizeHealthChecks(catalog.Identity.Kind, buildIdentityHealthChecks(ctx, catalog.Identity, idpConfig)),
		Notifications: summarizeHealthChecks(catalog.Notifications.Kind, buildNotificationsHealthChecks(catalog.Notifications, settings)),
	}
}

func buildSCMHealthChecks(ctx context.Context, category platformmodels.ProviderCategory, documents []bson.M) []platformmodels.ProviderHealthCheck {
	checks := make([]platformmodels.ProviderHealthCheck, 0, len(documents))
	for _, document := range documents {
		providerID := scmproviders.Normalize(shared.StringValue(document["provider"]))
		definition := findProviderDefinition(category, providerID)
		check := platformmodels.ProviderHealthCheck{
			ProviderID:     providerID,
			ProviderLabel:  definition.Label,
			ResourceID:     resourceID(document),
			ResourceLabel:  resourceLabel(document),
			Scope:          shared.StringValue(document["scope"]),
			Implementation: definition.Implementation,
		}

		authType := strings.ToLower(strings.TrimSpace(shared.StringValue(document["authType"])))
		if authType == "" {
			authType = "token"
		}
		if authType != "token" {
			check.State = providerHealthUnsupported
			check.Message = "Healthcheck currently supports token-based SCM credentials only"
			checks = append(checks, check)
			continue
		}

		runtime, err := scmproviders.ResolveRuntime(providerID)
		if err != nil {
			check.State = providerHealthUnsupported
			check.Message = err.Error()
			checks = append(checks, check)
			continue
		}
		if err := runtime.HealthCheck(ctx, shared.StringValue(document["token"])); err != nil {
			check.State = providerHealthUnhealthy
			check.Message = err.Error()
		} else {
			check.State = providerHealthHealthy
			check.Message = "Credential validated successfully"
		}
		checks = append(checks, check)
	}
	return checks
}

func buildRegistryHealthChecks(ctx context.Context, category platformmodels.ProviderCategory, documents []bson.M) []platformmodels.ProviderHealthCheck {
	checks := make([]platformmodels.ProviderHealthCheck, 0, len(documents))
	for _, document := range documents {
		providerID := registryproviders.Normalize(shared.StringValue(document["provider"]))
		definition := findProviderDefinition(category, providerID)
		check := platformmodels.ProviderHealthCheck{
			ProviderID:     providerID,
			ProviderLabel:  definition.Label,
			ResourceID:     resourceID(document),
			ResourceLabel:  resourceLabel(document),
			Scope:          shared.StringValue(document["scope"]),
			Implementation: definition.Implementation,
		}

		runtime, err := registryproviders.ResolveRuntime(providerID)
		if err != nil {
			check.State = providerHealthUnsupported
			check.Message = err.Error()
			checks = append(checks, check)
			continue
		}
		if err := runtime.HealthCheck(ctx, map[string]interface{}(document)); err != nil {
			check.State = providerHealthUnhealthy
			check.Message = err.Error()
		} else {
			check.State = providerHealthHealthy
			check.Message = "Registry endpoint validated successfully"
		}
		checks = append(checks, check)
	}
	return checks
}

func buildSecretsHealthChecks(ctx context.Context, category platformmodels.ProviderCategory, settings bson.M) []platformmodels.ProviderHealthCheck {
	secrets := nestedMap(settings, "secrets")
	defaultProviderID := strings.TrimSpace(shared.StringValue(secrets["defaultProviderId"]))
	items := interfaceSlice(secrets["providers"])
	checks := make([]platformmodels.ProviderHealthCheck, 0, len(items))
	for _, item := range items {
		document := bsonMap(item)
		provider := secretsproviders.ParseProvider(document)
		if provider == nil {
			continue
		}
		definition := findProviderDefinition(category, provider.Type)
		check := platformmodels.ProviderHealthCheck{
			ProviderID:     provider.Type,
			ProviderLabel:  definition.Label,
			ResourceID:     provider.ID,
			ResourceLabel:  resourceLabel(document),
			State:          providerHealthUnsupported,
			Default:        provider.ID != "" && provider.ID == defaultProviderID,
			Implementation: definition.Implementation,
		}

		runtime, ok := secretsproviders.ResolveRuntime(provider.Type)
		if !ok {
			check.Message = "Provider runtime is unavailable"
			checks = append(checks, check)
			continue
		}
		if err := runtime.HealthCheck(ctx, provider); err != nil {
			check.State = providerHealthUnhealthy
			check.Message = err.Error()
		} else {
			check.State = providerHealthHealthy
			check.Message = "Secret provider validated successfully"
		}
		checks = append(checks, check)
	}
	return checks
}

func buildIdentityHealthChecks(ctx context.Context, category platformmodels.ProviderCategory, idpConfig bson.M) []platformmodels.ProviderHealthCheck {
	checks := make([]platformmodels.ProviderHealthCheck, 0, len(category.Providers))
	for _, provider := range category.Providers {
		config := nestedMap(idpConfig, provider.ID)
		enabled := boolValue(config["enabled"])
		check := platformmodels.ProviderHealthCheck{
			ProviderID:     provider.ID,
			ProviderLabel:  provider.Label,
			ResourceID:     provider.ID,
			ResourceLabel:  provider.Label,
			Implementation: provider.Implementation,
		}
		if !enabled {
			check.State = providerHealthDisabled
			check.Message = "Protocol disabled"
			checks = append(checks, check)
			continue
		}

		runtime, err := identityproviders.ResolveRuntime(provider.ID)
		if err != nil {
			check.State = providerHealthUnsupported
			check.Message = err.Error()
			checks = append(checks, check)
			continue
		}
		if err := runtime.ValidateConfiguration(config); err != nil {
			check.State = providerHealthUnhealthy
			check.Message = err.Error()
			checks = append(checks, check)
			continue
		}
		if err := runtime.TestConnection(ctx, config); err != nil {
			check.State = providerHealthUnhealthy
			check.Message = err.Error()
		} else {
			check.State = providerHealthHealthy
			check.Message = "Identity provider reachable"
		}
		checks = append(checks, check)
	}
	return checks
}

func buildNotificationsHealthChecks(category platformmodels.ProviderCategory, settings bson.M) []platformmodels.ProviderHealthCheck {
	notifications := nestedMap(settings, "notifications")
	provider := category.Providers[0]
	check := platformmodels.ProviderHealthCheck{
		ProviderID:     provider.ID,
		ProviderLabel:  provider.Label,
		ResourceID:     provider.ID,
		ResourceLabel:  provider.Label,
		Default:        provider.ID == category.DefaultProvider,
		Implementation: provider.Implementation,
	}

	runtime, err := notificationproviders.ResolveRuntime(provider.ID)
	if err != nil {
		check.State = providerHealthUnsupported
		check.Message = err.Error()
		return []platformmodels.ProviderHealthCheck{check}
	}
	enabled := runtime.EnabledResourceCount(map[string]interface{}(notifications))
	if enabled == 0 {
		check.State = providerHealthDisabled
		check.Message = "All notification events are disabled"
	} else {
		check.State = providerHealthHealthy
		check.Message = "Notification provider is active"
	}
	return []platformmodels.ProviderHealthCheck{check}
}

func summarizeHealthChecks(kind string, checks []platformmodels.ProviderHealthCheck) platformmodels.ProviderHealthCategory {
	category := platformmodels.ProviderHealthCategory{
		Kind:   kind,
		Checks: checks,
	}
	for _, check := range checks {
		switch check.State {
		case providerHealthHealthy:
			category.Healthy++
		case providerHealthUnhealthy:
			category.Unhealthy++
		case providerHealthUnsupported:
			category.Unsupported++
		case providerHealthDisabled:
			category.Disabled++
		}
	}
	return category
}

func findProviderDefinition(category platformmodels.ProviderCategory, providerID string) platformmodels.ProviderDefinition {
	for _, provider := range category.Providers {
		if provider.ID == providerID {
			return provider
		}
	}
	return platformmodels.ProviderDefinition{
		ID:    providerID,
		Label: strings.TrimSpace(providerID),
	}
}

func resourceLabel(document bson.M) string {
	if name := strings.TrimSpace(shared.StringValue(document["name"])); name != "" {
		return name
	}
	if id := resourceID(document); id != "" {
		return id
	}
	return "unnamed-resource"
}

func resourceID(document bson.M) string {
	if id := strings.TrimSpace(shared.StringValue(document["id"])); id != "" {
		return id
	}
	return strings.TrimSpace(shared.StringValue(document["_id"]))
}
