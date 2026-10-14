package scm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	scmmodels "releaseaapi/internal/platform/models"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	templateSourceOwner = "releasea"
	templateSourceRepo  = "templates"
)

func CheckTemplateRepoAvailability(c *gin.Context) {
	owner := strings.TrimSpace(c.Query("owner"))
	name := strings.TrimSpace(c.Query("name"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	scmCredentialID := strings.TrimSpace(c.Query("scmCredentialId"))

	if owner == "" || name == "" {
		shared.RespondError(c, http.StatusBadRequest, "owner and name are required")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()

	runtime, token, statusCode, err := resolveScmRuntimeToken(ctx, scmCredentialID, projectID, scmproviders.CapabilityTemplateRepo)
	if err != nil {
		shared.RespondError(c, statusCode, err.Error())
		return
	}

	exists, err := runtime.CheckTemplateRepoAvailability(ctx, token, owner, name)
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"owner":    owner,
		"name":     name,
		"fullName": owner + "/" + name,
		"exists":   exists,
	})
}

func CreateTemplateRepo(c *gin.Context) {
	var payload scmmodels.TemplateRepoRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	if err := normalizeTemplateRepoPayload(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	runtime, token, statusCode, err := resolveScmRuntimeToken(ctx, payload.ScmCredentialID, payload.ProjectID, scmproviders.CapabilityTemplateRepo)
	if err != nil {
		shared.RespondError(c, statusCode, err.Error())
		return
	}

	repo, err := createTemplateRepoWithRetry(ctx, runtime, token, payload)
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, repo)
}

func createTemplateRepoWithRetry(ctx context.Context, runtime scmproviders.Runtime, token string, payload scmmodels.TemplateRepoRequest) (*scmmodels.TemplateRepoResponse, error) {
	var repo *scmmodels.TemplateRepoResponse
	var err error

	maxAttempts := 3
	baseDelay := 2 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		repo, err = runtime.CreateTemplateRepo(ctx, token, payload)
		if err == nil {
			return repo, nil
		}

		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "already exists") {
			existingRepo, fetchErr := fetchExistingRepo(ctx, runtime, token, payload.Owner, payload.Name)
			if fetchErr == nil && existingRepo != nil {
				return existingRepo, nil
			}
			return nil, err
		}

		if attempt < maxAttempts {
			delay := baseDelay * time.Duration(1<<(attempt-1))
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
			continue
		}
	}

	return nil, err
}

func fetchExistingRepo(ctx context.Context, runtime scmproviders.Runtime, token, owner, name string) (*scmmodels.TemplateRepoResponse, error) {
	ghRuntime, ok := runtime.(interface {
		CheckTemplateRepoAvailability(ctx context.Context, token, owner, name string) (bool, error)
	})
	if !ok {
		return nil, fmt.Errorf("runtime does not support repo availability check")
	}

	exists, err := ghRuntime.CheckTemplateRepoAvailability(ctx, token, owner, name)
	if err != nil || !exists {
		return nil, fmt.Errorf("repository not found")
	}

	return nil, fmt.Errorf("repository exists but cannot fetch details")
}

func resolveScmCredential(ctx context.Context, scmCredentialID string, projectID string) (bson.M, error) {
	if strings.TrimSpace(scmCredentialID) != "" {
		return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": scmCredentialID})
	}
	if strings.TrimSpace(projectID) != "" {
		project, err := shared.FindOne(ctx, shared.Collection(shared.ProjectsCollection), bson.M{"id": projectID})
		if err == nil && project != nil {
			if id := shared.StringValue(project["scmCredentialId"]); id != "" {
				return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
			}
		}
	}
	return shared.FindLatestPlatformCredential(ctx, shared.ScmCredentialsCollection)
}

func resolveScmRuntimeToken(ctx context.Context, scmCredentialID, projectID, capability string) (scmproviders.Runtime, string, int, error) {
	scmCred, err := resolveScmCredential(ctx, scmCredentialID, projectID)
	if err != nil {
		return nil, "", http.StatusNotFound, errors.New("scm credential not found")
	}
	provider := strings.ToLower(shared.StringValue(scmCred["provider"]))
	runtime, err := scmproviders.ResolveRuntimeForCapability(provider, capability)
	if err != nil {
		return nil, "", http.StatusBadRequest, err
	}
	token, err := shared.DecryptSensitiveValue(strings.TrimSpace(shared.StringValue(scmCred["token"])))
	if err != nil {
		return nil, "", http.StatusInternalServerError, errors.New("failed to decrypt scm credential")
	}
	if token == "" {
		return nil, "", http.StatusBadRequest, errors.New("scm credential missing token")
	}
	return runtime, token, 0, nil
}

func normalizeTemplateRepoPayload(payload *scmmodels.TemplateRepoRequest) error {
	payload.TemplatePath = strings.TrimSpace(payload.TemplatePath)
	payload.Owner = strings.TrimSpace(payload.Owner)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)
	if payload.TemplatePath == "" {
		return errors.New("template path required")
	}
	if payload.Owner == "" || payload.Name == "" {
		return errors.New("new repository owner and name required")
	}
	payload.TemplateOwner = templateSourceOwner
	payload.TemplateRepo = templateSourceRepo
	return nil
}

func ListCommits(c *gin.Context) {
	owner := strings.TrimSpace(c.Query("owner"))
	repo := strings.TrimSpace(c.Query("repo"))
	repoURL := strings.TrimSpace(c.Query("repoUrl"))
	branch := strings.TrimSpace(c.Query("branch"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	scmCredentialID := strings.TrimSpace(c.Query("scmCredentialId"))

	if (owner == "" || repo == "") && repoURL == "" {
		shared.RespondError(c, http.StatusBadRequest, "owner and repo or repoUrl are required")
		return
	}
	if branch == "" {
		branch = "main"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()

	runtime, token, statusCode, err := resolveScmRuntimeToken(ctx, scmCredentialID, projectID, scmproviders.CapabilityCommitLookup)
	if err != nil {
		shared.RespondError(c, statusCode, err.Error())
		return
	}

	var commits []scmmodels.CommitEntry
	if repoURL != "" && (owner == "" || repo == "") {
		commits, err = runtime.ListCommitsByRepoURL(ctx, token, repoURL, branch)
	} else {
		commits, err = runtime.ListCommits(ctx, token, owner, repo, branch)
	}
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}

	c.JSON(http.StatusOK, commits)
}
