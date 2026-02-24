package projects

import (
	"context"
	"fmt"
	"strings"

	gh "releaseaapi/api/v1/integrations/github"
	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func cleanupManagedGithubRepos(ctx context.Context, projectID string, project bson.M, services []bson.M) error {
	repos := make(map[string]gh.RepoRef)
	tokens := make(map[string]string)

	for _, service := range services {
		repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
		if repoURL == "" {
			continue
		}
		sourceType := strings.ToLower(strings.TrimSpace(shared.StringValue(service["sourceType"])))
		if sourceType == "registry" || sourceType == "docker" {
			continue
		}
		repo, ok := gh.ParseRepo(repoURL)
		if !ok {
			continue
		}
		key := fmt.Sprintf("%s/%s", repo.Owner, repo.Name)
		if _, exists := repos[key]; exists {
			continue
		}
		token, err := resolveServiceScmToken(ctx, service, project, key)
		if err != nil {
			return err
		}
		if token == "" {
			return fmt.Errorf("SCM credential missing token for repository %s", key)
		}
		repos[key] = repo
		tokens[key] = token
	}

	for key, repo := range repos {
		token := tokens[key]
		_, err := gh.DeleteManagedRepo(ctx, token, repo, projectID, false)
		if err != nil {
			return err
		}
	}

	return nil
}

func resolveServiceScmToken(ctx context.Context, service bson.M, project bson.M, repoKey string) (string, error) {
	cred, err := resolveServiceScmCredential(ctx, service, project)
	if err != nil {
		return "", err
	}
	if cred == nil {
		return "", fmt.Errorf("SCM credential not found for repository %s", repoKey)
	}
	provider := strings.ToLower(shared.StringValue(cred["provider"]))
	if provider != "" && provider != "github" {
		return "", fmt.Errorf("SCM credential must be GitHub to delete repository %s", repoKey)
	}
	return strings.TrimSpace(shared.StringValue(cred["token"])), nil
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
