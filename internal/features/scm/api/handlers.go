package scm

import (
	"context"
	"errors"
	"net/http"
	"strings"

	scmmodels "releaseaapi/internal/features/scm/models"
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

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
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

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	runtime, token, statusCode, err := resolveScmRuntimeToken(ctx, payload.ScmCredentialID, payload.ProjectID, scmproviders.CapabilityTemplateRepo)
	if err != nil {
		shared.RespondError(c, statusCode, err.Error())
		return
	}

	repo, err := runtime.CreateTemplateRepo(ctx, token, payload)
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, repo)
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
	token := strings.TrimSpace(shared.StringValue(scmCred["token"]))
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

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
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
