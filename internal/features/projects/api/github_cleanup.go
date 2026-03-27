package projects

import (
	"context"
	"fmt"
	"strings"

	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func cleanupManagedScmRepos(ctx context.Context, projectID string, project bson.M, services []bson.M) error {
	repos := make(map[string]string)
	tokens := make(map[string]string)
	runtimes := make(map[string]scmproviders.Runtime)

	for _, service := range services {
		repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
		if repoURL == "" {
			continue
		}
		sourceType := strings.ToLower(strings.TrimSpace(shared.StringValue(service["sourceType"])))
		if sourceType == "registry" || sourceType == "docker" {
			continue
		}
		key := repoURL
		if _, exists := repos[key]; exists {
			continue
		}
		runtime, token, err := resolveServiceScmRuntimeToken(ctx, service, project, key)
		if err != nil {
			return err
		}
		if token == "" {
			return fmt.Errorf("SCM credential missing token for repository %s", key)
		}
		repos[key] = repoURL
		tokens[key] = token
		runtimes[key] = runtime
	}

	for key, repoURL := range repos {
		token := tokens[key]
		runtime := runtimes[key]
		_, err := runtime.DeleteManagedRepo(ctx, token, repoURL, projectID, false)
		if err != nil {
			return err
		}
	}

	return nil
}

func resolveServiceScmRuntimeToken(ctx context.Context, service bson.M, project bson.M, repoKey string) (scmproviders.Runtime, string, error) {
	cred, err := resolveServiceScmCredential(ctx, service, project)
	if err != nil {
		return nil, "", err
	}
	if cred == nil {
		return nil, "", fmt.Errorf("SCM credential not found for repository %s", repoKey)
	}
	provider := strings.ToLower(shared.StringValue(cred["provider"]))
	runtime, err := scmproviders.ResolveRuntimeForCapability(provider, scmproviders.CapabilityManagedDelete)
	if err != nil {
		return nil, "", fmt.Errorf("SCM credential provider %q cannot delete managed repository %s: %w", strings.TrimSpace(provider), repoKey, err)
	}
	return runtime, strings.TrimSpace(shared.StringValue(cred["token"])), nil
}

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
