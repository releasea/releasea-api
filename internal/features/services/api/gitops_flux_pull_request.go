package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	scmmodels "releaseaapi/internal/features/scm/models"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type gitOpsFluxPullRequestPayload struct {
	BaseBranch    string `json:"baseBranch"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	CommitMessage string `json:"commitMessage"`
}

var openServiceFluxStarterPullRequest = func(
	ctx context.Context,
	service bson.M,
	rules []bson.M,
	payload gitOpsFluxPullRequestPayload,
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
		return nil, errors.New("service repository URL is required for Flux GitOps pull requests")
	}

	exportData, err := buildServiceDesiredStateExport(service, rules)
	if err != nil {
		return nil, fmt.Errorf("failed to render desired state: %w", err)
	}
	if exportData.Validation.Status == "invalid" {
		return nil, serviceDesiredStateValidationError{Validation: exportData.Validation}
	}

	serviceName := shared.StringValue(service["name"])
	baseBranch := defaultGitOpsBaseBranch(service, payload.BaseBranch)
	request := scmmodels.DesiredStatePullRequestRequest{
		RepoURL:       repoURL,
		BaseBranch:    baseBranch,
		BranchName:    fmt.Sprintf("releasea/gitops/flux/%s-%s", sanitizeGitOpsPathSegment(serviceName), nowForGitOpsPullRequest().Format("20060102150405")),
		FilePath:      defaultGitOpsArgoCDDesiredStateFilePath(serviceName),
		Content:       exportData.YAML,
		CommitMessage: defaultGitOpsFluxCommitMessage(serviceName, payload.CommitMessage),
		Title:         defaultGitOpsFluxPRTitle(serviceName, payload.Title),
		Body:          defaultGitOpsFluxPRBody(service, exportData.Warnings, payload.Body),
		AdditionalFiles: []scmmodels.DesiredStatePullRequestFile{
			{
				Path:    defaultGitOpsArgoCDKustomizationFilePath(serviceName),
				Content: buildArgoCDStarterKustomization(service),
			},
			{
				Path:    defaultGitOpsFluxGitRepositoryFilePath(serviceName),
				Content: buildFluxStarterGitRepository(service, baseBranch),
			},
			{
				Path:    defaultGitOpsFluxKustomizationFilePath(serviceName),
				Content: buildFluxStarterKustomization(service),
			},
		},
	}

	return runtime.CreateDesiredStatePullRequest(ctx, token, request)
}

func CreateServiceFluxGitOpsPullRequest(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	var payload gitOpsFluxPullRequestPayload
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
			"message": "Flux GitOps pull request delivery is only available for managed services.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return
	}
	if strings.TrimSpace(shared.StringValue(service["repoUrl"])) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service repository URL is required for Flux GitOps pull requests")
		return
	}
	if err := ensureGitOpsRepositoryPolicyReady(ctx, service); err != nil {
		var repositoryPolicyErr serviceGitOpsRepositoryPolicyError
		if errors.As(err, &repositoryPolicyErr) {
			c.JSON(http.StatusConflict, gin.H{
				"message": err.Error(),
				"code":    "GITOPS_REPOSITORY_POLICY_INVALID",
				"policy":  repositoryPolicyErr.Check,
			})
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to evaluate GitOps repository policy")
		return
	}

	rules, err := findRulesForDesiredState(ctx, serviceID)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service rules")
		return
	}

	pr, err := openServiceFluxStarterPullRequest(ctx, service, rules, payload)
	if err != nil {
		var validationErr serviceDesiredStateValidationError
		if errors.As(err, &validationErr) {
			c.JSON(http.StatusConflict, gin.H{
				"message":    validationErr.Validation.Summary,
				"code":       "GITOPS_DESIRED_STATE_INVALID",
				"validation": validationErr.Validation,
			})
			return
		}
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}

	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "service.gitops_flux_pr.create",
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
			"filePaths":  pr.FilePaths,
		},
	})

	c.JSON(http.StatusOK, pr)
}

func buildFluxStarterGitRepository(service bson.M, baseBranch string) string {
	serviceName := sanitizeGitOpsPathSegment(shared.StringValue(service["name"]))
	repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
	if strings.HasPrefix(repoURL, "https://github.com/") && !strings.HasSuffix(repoURL, ".git") {
		repoURL += ".git"
	}
	return strings.TrimSpace(fmt.Sprintf(`
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: releasea-%s
  namespace: flux-system
  labels:
    app.kubernetes.io/part-of: releasea
    releasea.io/service-id: %s
    releasea.io/service-name: %s
spec:
  interval: 1m0s
  url: %s
  ref:
    branch: %s
`, serviceName, shared.StringValue(service["id"]), serviceName, repoURL, baseBranch)) + "\n"
}

func buildFluxStarterKustomization(service bson.M) string {
	serviceName := sanitizeGitOpsPathSegment(shared.StringValue(service["name"]))
	return strings.TrimSpace(fmt.Sprintf(`
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: releasea-%s
  namespace: flux-system
  labels:
    app.kubernetes.io/part-of: releasea
    releasea.io/service-id: %s
    releasea.io/service-name: %s
spec:
  interval: 10m0s
  path: ./.releasea/gitops/%s
  prune: false
  wait: false
  targetNamespace: releasea-system
  sourceRef:
    kind: GitRepository
    name: releasea-%s
`, serviceName, shared.StringValue(service["id"]), serviceName, serviceName, serviceName)) + "\n"
}
