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

type gitOpsArgoCDPullRequestPayload struct {
	BaseBranch    string `json:"baseBranch"`
	Title         string `json:"title"`
	Body          string `json:"body"`
	CommitMessage string `json:"commitMessage"`
}

var openServiceArgoCDStarterPullRequest = func(
	ctx context.Context,
	service bson.M,
	rules []bson.M,
	payload gitOpsArgoCDPullRequestPayload,
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
		return nil, errors.New("service repository URL is required for Argo CD GitOps pull requests")
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
		BranchName:    fmt.Sprintf("releasea/gitops/argocd/%s-%s", sanitizeGitOpsPathSegment(serviceName), nowForGitOpsPullRequest().Format("20060102150405")),
		FilePath:      defaultGitOpsArgoCDDesiredStateFilePath(serviceName),
		Content:       exportData.YAML,
		CommitMessage: defaultGitOpsArgoCDCommitMessage(serviceName, payload.CommitMessage),
		Title:         defaultGitOpsArgoCDPRTitle(serviceName, payload.Title),
		Body:          defaultGitOpsArgoCDPRBody(service, exportData.Warnings, payload.Body),
		AdditionalFiles: []scmmodels.DesiredStatePullRequestFile{
			{
				Path:    defaultGitOpsArgoCDKustomizationFilePath(serviceName),
				Content: buildArgoCDStarterKustomization(service),
			},
			{
				Path:    defaultGitOpsArgoCDApplicationFilePath(serviceName),
				Content: buildArgoCDStarterApplication(service, baseBranch),
			},
		},
	}

	return runtime.CreateDesiredStatePullRequest(ctx, token, request)
}

func CreateServiceArgoCDGitOpsPullRequest(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	var payload gitOpsArgoCDPullRequestPayload
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
			"message": "Argo CD GitOps pull request delivery is only available for managed services.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return
	}
	if strings.TrimSpace(shared.StringValue(service["repoUrl"])) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service repository URL is required for Argo CD GitOps pull requests")
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

	pr, err := openServiceArgoCDStarterPullRequest(ctx, service, rules, payload)
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
		Action:       "service.gitops_argocd_pr.create",
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

func buildArgoCDStarterKustomization(service bson.M) string {
	serviceName := sanitizeGitOpsPathSegment(shared.StringValue(service["name"]))
	serviceID := shared.StringValue(service["id"])
	return strings.TrimSpace(fmt.Sprintf(`
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
namespace: releasea-system
configMapGenerator:
  - name: releasea-desired-state-%s
    files:
      - desired-state.yaml
generatorOptions:
  disableNameSuffixHash: true
  labels:
    app.kubernetes.io/name: releasea-gitops-state
    app.kubernetes.io/part-of: releasea
    app.kubernetes.io/managed-by: argocd
    releasea.io/service-id: %s
    releasea.io/service-name: %s
`, serviceName, serviceID, serviceName)) + "\n"
}

func buildArgoCDStarterApplication(service bson.M, baseBranch string) string {
	serviceName := sanitizeGitOpsPathSegment(shared.StringValue(service["name"]))
	repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
	return strings.TrimSpace(fmt.Sprintf(`
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: releasea-%s
  namespace: argocd
  labels:
    app.kubernetes.io/part-of: releasea
    releasea.io/service-id: %s
    releasea.io/service-name: %s
spec:
  project: default
  source:
    repoURL: %s
    targetRevision: %s
    path: .releasea/gitops/%s
  destination:
    server: https://kubernetes.default.svc
    namespace: releasea-system
  syncPolicy:
    syncOptions:
      - CreateNamespace=true
`, serviceName, shared.StringValue(service["id"]), serviceName, repoURL, baseBranch, serviceName)) + "\n"
}
