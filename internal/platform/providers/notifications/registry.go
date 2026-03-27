package notificationproviders

import (
	"fmt"
	"strings"

	platformmodels "releaseaapi/internal/platform/models"
)

type Definition struct {
	ID             string
	Label          string
	Description    string
	Capabilities   []string
	Implementation string
}

type Runtime interface {
	ID() string
	ValidateConfiguration(config map[string]interface{}) error
	EnabledResourceCount(config map[string]interface{}) int
}

var registry = map[string]Definition{
	"platform-events": {
		ID:             "platform-events",
		Label:          "Platform Events",
		Description:    "Built-in notification toggles for deploy, service, worker and approval events.",
		Capabilities:   []string{"deploy-success", "deploy-failed", "service-down", "worker-offline", "approval-required", "approval-completed"},
		Implementation: "platform-events",
	},
}

func Normalize(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func Resolve(provider string) (Definition, bool) {
	definition, ok := registry[Normalize(provider)]
	return definition, ok
}

func ResolveRuntime(provider string) (Runtime, error) {
	switch Normalize(provider) {
	case "platform-events":
		return platformEventsRuntime{}, nil
	default:
		return nil, fmt.Errorf("unsupported notification provider %q", strings.TrimSpace(provider))
	}
}

func CatalogCategory() platformmodels.ProviderCategory {
	definition := registry["platform-events"]
	return platformmodels.ProviderCategory{
		Kind:            "notifications",
		Label:           "Notifications",
		Description:     "Built-in platform event notifications currently configurable in platform settings.",
		DefaultProvider: "platform-events",
		Providers: []platformmodels.ProviderDefinition{
			{
				ID:             definition.ID,
				Label:          definition.Label,
				Description:    definition.Description,
				Capabilities:   definition.Capabilities,
				Implementation: definition.Implementation,
			},
		},
	}
}
