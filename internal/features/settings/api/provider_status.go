package settings

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	platformmodels "releaseaapi/internal/platform/models"
	identityproviders "releaseaapi/internal/platform/providers/identity"
	notificationproviders "releaseaapi/internal/platform/providers/notifications"
	registryproviders "releaseaapi/internal/platform/providers/registry"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	providerStateConfigured    = "configured"
	providerStatePartial       = "partial"
	providerStateNotConfigured = "not-configured"
	providerStateDisabled      = "disabled"
)

func GetProviderStatus(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	status, err := loadProviderStatusCatalog(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load provider status")
		return
	}
	c.JSON(http.StatusOK, status)
}

func loadProviderStatusCatalog(ctx context.Context) (platformmodels.ProviderStatusCatalog, error) {
	settings, err := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{})
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return platformmodels.ProviderStatusCatalog{}, err
	}
	if settings == nil {
		settings = bson.M{}
	}

	idpConfig, err := shared.FindOne(ctx, shared.Collection(shared.IdpConfigCollection), bson.M{})
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return platformmodels.ProviderStatusCatalog{}, err
	}
	if idpConfig == nil {
		idpConfig = bson.M{}
	}

	scmCredentials, err := shared.FindAll(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{})
	if err != nil {
		return platformmodels.ProviderStatusCatalog{}, err
	}
	registryCredentials, err := shared.FindAll(ctx, shared.Collection(shared.RegistryCredentialsCollection), bson.M{})
	if err != nil {
		return platformmodels.ProviderStatusCatalog{}, err
	}

	return buildProviderStatusCatalog(buildProviderCatalog(), settings, idpConfig, scmCredentials, registryCredentials), nil
}

func buildProviderStatusCatalog(
	catalog platformmodels.ProviderCatalog,
	settings bson.M,
	idpConfig bson.M,
	scmCredentials []bson.M,
	registryCredentials []bson.M,
) platformmodels.ProviderStatusCatalog {
	return platformmodels.ProviderStatusCatalog{
		Version: catalog.Version,
		SCM: platformmodels.ProviderStatusCategory{
			Kind:      catalog.SCM.Kind,
			Providers: buildCredentialProviderStatuses(catalog.SCM, scmCredentials, func(doc bson.M) string { return scmproviders.Normalize(shared.StringValue(doc["provider"])) }),
		},
		Registry: platformmodels.ProviderStatusCategory{
			Kind:      catalog.Registry.Kind,
			Providers: buildCredentialProviderStatuses(catalog.Registry, registryCredentials, func(doc bson.M) string { return registryproviders.Normalize(shared.StringValue(doc["provider"])) }),
		},
		Secrets: platformmodels.ProviderStatusCategory{
			Kind:      catalog.Secrets.Kind,
			Providers: buildSecretProviderStatuses(catalog.Secrets, settings),
		},
		Identity: platformmodels.ProviderStatusCategory{
			Kind:      catalog.Identity.Kind,
			Providers: buildIdentityProviderStatuses(catalog.Identity, idpConfig),
		},
		Notifications: platformmodels.ProviderStatusCategory{
			Kind:      catalog.Notifications.Kind,
			Providers: buildNotificationProviderStatuses(catalog.Notifications, settings),
		},
	}
}

func buildCredentialProviderStatuses(
	category platformmodels.ProviderCategory,
	documents []bson.M,
	resolveProvider func(bson.M) string,
) []platformmodels.ProviderStatus {
	counts := map[string]int{}
	for _, document := range documents {
		provider := strings.TrimSpace(resolveProvider(document))
		if provider == "" {
			continue
		}
		counts[provider]++
	}

	statuses := make([]platformmodels.ProviderStatus, 0, len(category.Providers))
	for _, provider := range category.Providers {
		count := counts[provider.ID]
		state := providerStateNotConfigured
		message := "No credentials configured"
		if count > 0 {
			state = providerStateConfigured
			if count == 1 {
				message = "1 credential configured"
			} else {
				message = fmt.Sprintf("%d credentials configured", count)
			}
		}
		statuses = append(statuses, platformmodels.ProviderStatus{
			ID:            provider.ID,
			Label:         provider.Label,
			State:         state,
			Message:       message,
			Configured:    count > 0,
			Default:       provider.ID == category.DefaultProvider,
			ResourceCount: count,
		})
	}
	return statuses
}

func buildSecretProviderStatuses(category platformmodels.ProviderCategory, settings bson.M) []platformmodels.ProviderStatus {
	secrets := nestedMap(settings, "secrets")
	defaultProviderID := shared.StringValue(secrets["defaultProviderId"])
	defaultProviderType := ""
	connectedCount := map[string]int{}
	totalCount := map[string]int{}

	for _, item := range interfaceSlice(secrets["providers"]) {
		provider := bsonMap(item)
		providerType := strings.ToLower(strings.TrimSpace(shared.StringValue(provider["type"])))
		if providerType == "" {
			continue
		}
		totalCount[providerType]++
		if shared.StringValue(provider["id"]) == defaultProviderID {
			defaultProviderType = providerType
		}
		if strings.EqualFold(shared.StringValue(provider["status"]), "connected") {
			connectedCount[providerType]++
		}
	}

	statuses := make([]platformmodels.ProviderStatus, 0, len(category.Providers))
	for _, provider := range category.Providers {
		total := totalCount[provider.ID]
		connected := connectedCount[provider.ID]
		state := providerStateNotConfigured
		message := "No secret providers configured"
		configured := false
		if total > 0 && connected == 0 {
			state = providerStatePartial
			message = fmt.Sprintf("%d provider records saved but not connected", total)
		}
		if connected > 0 {
			state = providerStateConfigured
			configured = true
			if connected == 1 {
				message = "1 connected secret provider"
			} else {
				message = fmt.Sprintf("%d connected secret providers", connected)
			}
		}
		statuses = append(statuses, platformmodels.ProviderStatus{
			ID:            provider.ID,
			Label:         provider.Label,
			State:         state,
			Message:       message,
			Configured:    configured,
			Default:       provider.ID == defaultProviderType || (provider.ID == category.DefaultProvider && defaultProviderType == ""),
			ResourceCount: total,
		})
	}
	return statuses
}

func buildIdentityProviderStatuses(category platformmodels.ProviderCategory, idpConfig bson.M) []platformmodels.ProviderStatus {
	statuses := make([]platformmodels.ProviderStatus, 0, len(category.Providers))
	for _, provider := range category.Providers {
		config := nestedMap(idpConfig, provider.ID)
		enabled := boolValue(config["enabled"])
		state := providerStateDisabled
		message := "Protocol disabled"
		configured := false

		runtime, err := identityproviders.ResolveRuntime(provider.ID)
		if err != nil {
			state = providerStatePartial
			message = "Provider runtime is unavailable"
		} else if enabled {
			if err := runtime.ValidateConfiguration(config); err != nil {
				state = providerStatePartial
				message = err.Error()
			} else {
				state = providerStateConfigured
				message = "Enabled and configured"
				configured = true
			}
		}

		statuses = append(statuses, platformmodels.ProviderStatus{
			ID:             provider.ID,
			Label:          provider.Label,
			State:          state,
			Message:        message,
			Configured:     configured,
			Default:        provider.ID == category.DefaultProvider,
			Implementation: provider.Implementation,
		})
	}
	return statuses
}

func buildNotificationProviderStatuses(category platformmodels.ProviderCategory, settings bson.M) []platformmodels.ProviderStatus {
	notifications := nestedMap(settings, "notifications")
	runtime, err := notificationproviders.ResolveRuntime(category.DefaultProvider)
	enabled := 0
	if err == nil {
		enabled = runtime.EnabledResourceCount(map[string]interface{}(notifications))
	}

	state := providerStateDisabled
	message := "All notification events are disabled"
	configured := false
	if err != nil {
		state = providerStatePartial
		message = "Provider runtime is unavailable"
	}
	if enabled > 0 {
		state = providerStateConfigured
		configured = true
		message = fmt.Sprintf("%d notification events enabled", enabled)
	}

	provider := category.Providers[0]
	return []platformmodels.ProviderStatus{
		{
			ID:            provider.ID,
			Label:         provider.Label,
			State:         state,
			Message:       message,
			Configured:    configured,
			Default:       provider.ID == category.DefaultProvider,
			ResourceCount: enabled,
		},
	}
}

func nestedMap(document bson.M, key string) bson.M {
	return bsonMap(document[key])
}

func bsonMap(value interface{}) bson.M {
	switch typed := value.(type) {
	case bson.M:
		return typed
	case map[string]interface{}:
		return bson.M(typed)
	default:
		return bson.M{}
	}
}

func interfaceSlice(value interface{}) []interface{} {
	if items, ok := value.([]interface{}); ok {
		return items
	}
	return nil
}

func boolValue(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	default:
		return false
	}
}
