package scmproviders

import (
	"context"
	"fmt"
	"io/fs"
	"slices"
	"strings"

	scmmodels "releaseaapi/internal/features/scm/models"
	platformmodels "releaseaapi/internal/platform/models"
)

const (
	CapabilityRepoClone     = "repo-clone"
	CapabilityTemplateRepo  = "template-repos"
	CapabilityCommitLookup  = "commit-history"
	CapabilityManagedDelete = "managed-delete"
	CapabilityPullRequests  = "pull-requests"
	CapabilityRepoFiles     = "repo-files"
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
	HealthCheck(ctx context.Context, token string) error
	CheckTemplateRepoAvailability(ctx context.Context, token, owner, name string) (bool, error)
	CreateTemplateRepo(ctx context.Context, token string, payload scmmodels.TemplateRepoRequest) (*scmmodels.TemplateRepoResponse, error)
	ListCommits(ctx context.Context, token, owner, repo, branch string) ([]scmmodels.CommitEntry, error)
	ListCommitsByRepoURL(ctx context.Context, token, repoURL, branch string) ([]scmmodels.CommitEntry, error)
	LatestCommitSHA(ctx context.Context, token, repoURL, branch string) (string, error)
	DeleteManagedRepo(ctx context.Context, token, repoURL, projectID string, allowWithoutMarker bool) (bool, error)
	CreateDesiredStatePullRequest(ctx context.Context, token string, payload scmmodels.DesiredStatePullRequestRequest) (*scmmodels.DesiredStatePullRequestResponse, error)
	ReadFileContent(ctx context.Context, token, repoURL, path, ref string) (string, error)
}

var ErrFileNotFound = fs.ErrNotExist

var registry = map[string]Definition{
	"github": {
		ID:           "github",
		Label:        "GitHub",
		Description:  "Built-in GitHub support used by templates, commit lookup and managed repository operations.",
		AuthModes:    []string{"token", "ssh"},
		Capabilities: []string{CapabilityRepoClone, CapabilityTemplateRepo, CapabilityCommitLookup, CapabilityManagedDelete, CapabilityPullRequests, CapabilityRepoFiles},
		ScopeSupport: []string{"platform", "project", "service"},
	},
	"gitlab": {
		ID:           "gitlab",
		Label:        "GitLab",
		Description:  "Credential option for repository cloning in worker build flows.",
		AuthModes:    []string{"token", "ssh"},
		Capabilities: []string{CapabilityRepoClone},
		ScopeSupport: []string{"platform", "project", "service"},
	},
	"bitbucket": {
		ID:           "bitbucket",
		Label:        "Bitbucket",
		Description:  "Credential option for repository cloning in worker build flows.",
		AuthModes:    []string{"token", "ssh"},
		Capabilities: []string{CapabilityRepoClone},
		ScopeSupport: []string{"platform", "project", "service"},
	},
}

func Normalize(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "" {
		return "github"
	}
	return provider
}

func Resolve(provider string) (Definition, bool) {
	definition, ok := registry[Normalize(provider)]
	return definition, ok
}

func MustResolve(provider string) (Definition, error) {
	definition, ok := Resolve(provider)
	if !ok {
		return Definition{}, fmt.Errorf("unsupported scm provider %q", strings.TrimSpace(provider))
	}
	return definition, nil
}

func ValidateCredential(provider, authType string) error {
	definition, err := MustResolve(provider)
	if err != nil {
		return err
	}
	authType = strings.ToLower(strings.TrimSpace(authType))
	if authType == "" {
		authType = "token"
	}
	if !slices.Contains(definition.AuthModes, authType) {
		return fmt.Errorf("unsupported auth type %q for scm provider %q", authType, definition.ID)
	}
	return nil
}

func SupportsCapability(provider, capability string) bool {
	definition, ok := Resolve(provider)
	if !ok {
		return false
	}
	return slices.Contains(definition.Capabilities, strings.TrimSpace(capability))
}

func ResolveRuntime(provider string) (Runtime, error) {
	switch Normalize(provider) {
	case "github":
		return githubRuntime{}, nil
	default:
		return nil, fmt.Errorf("runtime not implemented for scm provider %q", strings.TrimSpace(provider))
	}
}

func ResolveRuntimeForCapability(provider, capability string) (Runtime, error) {
	definition, err := MustResolve(provider)
	if err != nil {
		return nil, err
	}
	capability = strings.TrimSpace(capability)
	if !slices.Contains(definition.Capabilities, capability) {
		return nil, fmt.Errorf("scm provider %q does not support capability %q", definition.ID, capability)
	}
	return ResolveRuntime(definition.ID)
}

func CatalogCategory() platformmodels.ProviderCategory {
	definitions := make([]platformmodels.ProviderDefinition, 0, len(registry))
	for _, key := range []string{"github", "gitlab", "bitbucket"} {
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
		Kind:            "scm",
		Label:           "SCM",
		Description:     "Source control providers used for repository access and template operations.",
		DefaultProvider: "github",
		Providers:       definitions,
	}
}
