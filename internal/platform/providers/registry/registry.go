package registryproviders

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	platformmodels "releaseaapi/internal/platform/models"
)

type Definition struct {
	ID           string
	Label        string
	Description  string
	AuthModes    []string
	Capabilities []string
	ScopeSupport []string
}

type Runtime interface {
	ID() string
	HealthCheck(ctx context.Context, credential map[string]interface{}) error
}

var registry = map[string]Definition{
	"docker": {
		ID:           "docker",
		Label:        "Docker Registry",
		Description:  "Generic OCI registry using username and password authentication.",
		AuthModes:    []string{"basic"},
		Capabilities: []string{"image-push", "image-pull"},
		ScopeSupport: []string{"platform", "project", "service"},
	},
	"ghcr": {
		ID:           "ghcr",
		Label:        "GitHub Container Registry",
		Description:  "OCI registry hosted by GitHub with standard basic credentials.",
		AuthModes:    []string{"basic"},
		Capabilities: []string{"image-push", "image-pull"},
		ScopeSupport: []string{"platform", "project", "service"},
	},
	"ecr": {
		ID:           "ecr",
		Label:        "AWS ECR",
		Description:  "AWS Elastic Container Registry using generated username and password credentials.",
		AuthModes:    []string{"basic"},
		Capabilities: []string{"image-push", "image-pull"},
		ScopeSupport: []string{"platform", "project", "service"},
	},
	"gcr": {
		ID:           "gcr",
		Label:        "Google Container Registry",
		Description:  "Google-hosted registry using standard username and password credentials.",
		AuthModes:    []string{"basic"},
		Capabilities: []string{"image-push", "image-pull"},
		ScopeSupport: []string{"platform", "project", "service"},
	},
	"acr": {
		ID:           "acr",
		Label:        "Azure Container Registry",
		Description:  "Azure-hosted registry using standard username and password credentials.",
		AuthModes:    []string{"basic"},
		Capabilities: []string{"image-push", "image-pull"},
		ScopeSupport: []string{"platform", "project", "service"},
	},
}

func Normalize(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "docker"
	}
	return provider
}

func Resolve(provider string) (Definition, bool) {
	definition, ok := registry[Normalize(provider)]
	return definition, ok
}

func ValidateCredential(provider string) error {
	if _, ok := Resolve(provider); !ok {
		return fmt.Errorf("unsupported registry provider %q", strings.TrimSpace(provider))
	}
	return nil
}

func ResolveRuntime(provider string) (Runtime, error) {
	switch Normalize(provider) {
	case "docker":
		return basicRegistryRuntime{id: "docker", defaultURL: "https://registry-1.docker.io"}, nil
	case "ghcr":
		return basicRegistryRuntime{id: "ghcr", defaultURL: "https://ghcr.io"}, nil
	case "gcr":
		return basicRegistryRuntime{id: "gcr", defaultURL: "https://gcr.io"}, nil
	case "ecr":
		return basicRegistryRuntime{id: "ecr"}, nil
	case "acr":
		return basicRegistryRuntime{id: "acr"}, nil
	default:
		return nil, fmt.Errorf("unsupported registry provider %q", strings.TrimSpace(provider))
	}
}

func CatalogCategory() platformmodels.ProviderCategory {
	definitions := make([]platformmodels.ProviderDefinition, 0, len(registry))
	for _, key := range []string{"docker", "ghcr", "ecr", "gcr", "acr"} {
		definition := registry[key]
		definitions = append(definitions, platformmodels.ProviderDefinition{
			ID:           definition.ID,
			Label:        definition.Label,
			Description:  definition.Description,
			AuthModes:    definition.AuthModes,
			Capabilities: definition.Capabilities,
			ScopeSupport: definition.ScopeSupport,
		})
	}
	return platformmodels.ProviderCategory{
		Kind:            "registry",
		Label:           "Registry",
		Description:     "Container registries available to build and deploy pipelines.",
		DefaultProvider: "docker",
		Providers:       definitions,
	}
}

type basicRegistryRuntime struct {
	id         string
	defaultURL string
}

func (r basicRegistryRuntime) ID() string { return r.id }

func (r basicRegistryRuntime) HealthCheck(ctx context.Context, credential map[string]interface{}) error {
	username := strings.TrimSpace(stringValue(credential, "username"))
	password := strings.TrimSpace(stringValue(credential, "password"))
	if username == "" || password == "" {
		return fmt.Errorf("%s registry credentials missing username or password", strings.ToUpper(r.id))
	}

	endpoint, err := resolveEndpoint(strings.TrimSpace(stringValue(credential, "registryUrl")), r.defaultURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint, "/")+"/v2/", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(username, password)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 400:
		return nil
	case resp.StatusCode == http.StatusUnauthorized:
		return fmt.Errorf("registry authentication failed")
	default:
		return fmt.Errorf("registry endpoint returned status %d", resp.StatusCode)
	}
}

func resolveEndpoint(raw, fallback string) (string, error) {
	endpoint := strings.TrimSpace(raw)
	if endpoint == "" {
		endpoint = strings.TrimSpace(fallback)
	}
	if endpoint == "" {
		return "", fmt.Errorf("registry endpoint required for provider")
	}
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || strings.TrimSpace(parsed.Scheme) == "" || strings.TrimSpace(parsed.Host) == "" {
		return "", fmt.Errorf("registry endpoint must be a valid URL")
	}
	return strings.TrimRight(parsed.String(), "/"), nil
}

func stringValue(values map[string]interface{}, key string) string {
	value, _ := values[key]
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return ""
	}
}
