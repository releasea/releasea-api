package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	scmmodels "releaseaapi/internal/features/scm/models"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/yaml.v3"
)

type gitOpsPullRequestPayload struct {
	BaseBranch    string `json:"baseBranch"`
	FilePath      string `json:"filePath"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	CommitMessage string `json:"commitMessage"`
}

var nowForGitOpsPullRequest = func() time.Time {
	return time.Now().UTC()
}

var openServiceDesiredStatePullRequest = func(
	ctx context.Context,
	service bson.M,
	rules []bson.M,
	payload gitOpsPullRequestPayload,
) (*scmmodels.DesiredStatePullRequestResponse, error) {
	project, err := loadServiceProject(ctx, service)
	if err != nil {
		return nil, err
	}

	credential, err := resolveServiceScmCredential(ctx, service, project)
	if err != nil {
		return nil, err
	}
	if credential == nil {
		return nil, errors.New("SCM credential not found")
	}

	token := strings.TrimSpace(shared.StringValue(credential["token"]))
	if token == "" {
		return nil, errors.New("SCM credential missing token")
	}

	provider := strings.ToLower(strings.TrimSpace(shared.StringValue(credential["provider"])))
	runtime, err := scmproviders.ResolveRuntimeForCapability(provider, scmproviders.CapabilityPullRequests)
	if err != nil {
		return nil, err
	}

	repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
	if repoURL == "" {
		return nil, errors.New("service repository URL is required for GitOps pull requests")
	}

	document, warnings := buildServiceDesiredStateDocument(service, rules, nowForDesiredState())
	rendered, err := yaml.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("failed to render desired state: %w", err)
	}

	request := scmmodels.DesiredStatePullRequestRequest{
		RepoURL:       repoURL,
		BaseBranch:    defaultGitOpsBaseBranch(service, payload.BaseBranch),
		BranchName:    buildGitOpsBranchName(shared.StringValue(service["name"])),
		FilePath:      defaultGitOpsFilePath(shared.StringValue(service["name"]), payload.FilePath),
		Content:       string(rendered),
		CommitMessage: defaultGitOpsCommitMessage(shared.StringValue(service["name"]), payload.CommitMessage),
		Title:         defaultGitOpsPRTitle(shared.StringValue(service["name"]), payload.Title),
		Body:          defaultGitOpsPRBody(service, warnings, payload.Body),
	}
	return runtime.CreateDesiredStatePullRequest(ctx, token, request)
}

func CreateServiceGitOpsPullRequest(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	var payload gitOpsPullRequestPayload
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := findServiceForDesiredState(ctx, serviceID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			shared.RespondError(c, http.StatusNotFound, "Service not found")
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service")
		return
	}
	if isObservedService(service) {
		c.JSON(http.StatusConflict, gin.H{
			"message": "GitOps pull request delivery is only available for managed services.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return
	}
	if strings.TrimSpace(shared.StringValue(service["repoUrl"])) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service repository URL is required for GitOps pull requests")
		return
	}

	rules, err := findRulesForDesiredState(ctx, serviceID)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service rules")
		return
	}

	pr, err := openServiceDesiredStatePullRequest(ctx, service, rules, payload)
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}

	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "service.gitops_pr.create",
		ResourceType: "service",
		ResourceID:   serviceID,
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"title":      pr.Title,
			"url":        pr.URL,
			"baseBranch": pr.BaseBranch,
			"branchName": pr.BranchName,
			"filePath":   pr.FilePath,
		},
	})

	c.JSON(http.StatusOK, pr)
}
func defaultGitOpsBaseBranch(service bson.M, override string) string {
	baseBranch := strings.TrimSpace(override)
	if baseBranch != "" {
		return baseBranch
	}
	baseBranch = strings.TrimSpace(shared.StringValue(service["branch"]))
	if baseBranch == "" {
		baseBranch = "main"
	}
	return baseBranch
}

func buildGitOpsBranchName(serviceName string) string {
	return fmt.Sprintf("releasea/gitops/%s-%s", sanitizeGitOpsPathSegment(serviceName), nowForGitOpsPullRequest().Format("20060102150405"))
}

func defaultGitOpsFilePath(serviceName, override string) string {
	if trimmed := strings.Trim(strings.TrimSpace(override), "/"); trimmed != "" {
		return trimmed
	}
	return defaultGitOpsLegacyFilePath(serviceName)
}

func defaultGitOpsCommitMessage(serviceName, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("chore(gitops): update desired state for %s", strings.TrimSpace(serviceName))
}

func defaultGitOpsPRTitle(serviceName, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	return fmt.Sprintf("chore(gitops): update desired state for %s", strings.TrimSpace(serviceName))
}

func defaultGitOpsPRBody(service bson.M, warnings []string, override string) string {
	if trimmed := strings.TrimSpace(override); trimmed != "" {
		return trimmed
	}
	lines := []string{
		"## Releasea GitOps change request",
		"",
		"This pull request was created by Releasea to update the exported desired state document for the managed service.",
		"",
		"- Service: `" + shared.StringValue(service["name"]) + "`",
		"- Project ID: `" + shared.StringValue(service["projectId"]) + "`",
		"- Source type: `" + normalizeServiceSourceType(shared.StringValue(service["sourceType"])) + "`",
	}
	if len(warnings) > 0 {
		lines = append(lines, "", "### Notes")
		for _, warning := range warnings {
			lines = append(lines, "- "+warning)
		}
	}
	return strings.Join(lines, "\n")
}

func sanitizeGitOpsPathSegment(value string) string {
	slug := make([]rune, 0, len(value))
	lastDash := false
	for _, char := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case char >= 'a' && char <= 'z', char >= '0' && char <= '9':
			slug = append(slug, char)
			lastDash = false
		case !lastDash:
			slug = append(slug, '-')
			lastDash = true
		}
	}
	result := strings.Trim(string(slug), "-")
	if result == "" {
		return "service"
	}
	return result
}
