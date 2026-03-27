package identityproviders

import (
	"context"
	"fmt"
	"strings"

	platformmodels "releaseaapi/internal/platform/models"

	"go.mongodb.org/mongo-driver/bson"
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
	ValidateConfiguration(config bson.M) error
	TestConnection(ctx context.Context, config bson.M) error
}

var registry = map[string]Definition{
	"saml": {
		ID:             "saml",
		Label:          "SAML",
		Description:    "SAML 2.0 single sign-on with group mapping and audit events.",
		Capabilities:   []string{"single-sign-on", "group-mapping", "session-revocation"},
		Implementation: "saml",
	},
	"oidc": {
		ID:             "oidc",
		Label:          "OIDC",
		Description:    "Generic OpenID Connect support for providers such as Keycloak, Okta, Azure AD and Google.",
		Capabilities:   []string{"single-sign-on", "group-mapping", "session-revocation"},
		Implementation: "oidc",
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
	case "saml":
		return samlRuntime{}, nil
	case "oidc":
		return oidcRuntime{}, nil
	default:
		return nil, fmt.Errorf("unsupported identity provider %q", strings.TrimSpace(provider))
	}
}

func CatalogCategory() platformmodels.ProviderCategory {
	definitions := make([]platformmodels.ProviderDefinition, 0, len(registry))
	for _, key := range []string{"saml", "oidc"} {
		definition := registry[key]
		definitions = append(definitions, platformmodels.ProviderDefinition{
			ID:             definition.ID,
			Label:          definition.Label,
			Description:    definition.Description,
			Capabilities:   definition.Capabilities,
			Implementation: definition.Implementation,
		})
	}
	return platformmodels.ProviderCategory{
		Kind:        "identity",
		Label:       "Identity",
		Description: "Identity protocols supported by Releasea. Providers such as Keycloak, Okta, Azure AD, Google and Microsoft typically connect through OIDC.",
		Providers:   definitions,
	}
}
