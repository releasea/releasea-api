package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	gh "releaseaapi/internal/platform/integrations/github"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type serviceGitOpsRepositoryPolicyCheck struct {
	Status     string                                   `json:"status"`
	Summary    string                                   `json:"summary"`
	RepoURL    string                                   `json:"repoUrl"`
	BaseBranch string                                   `json:"baseBranch"`
	Provider   string                                   `json:"provider"`
	Checks     []serviceGitOpsRepositoryPolicyCheckItem `json:"checks"`
}

type serviceGitOpsRepositoryPolicyCheckItem struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	State   string `json:"state"`
	Message string `json:"message"`
}

type serviceGitOpsRepositoryPolicyError struct {
	Check serviceGitOpsRepositoryPolicyCheck
}

func (e serviceGitOpsRepositoryPolicyError) Error() string {
	if strings.TrimSpace(e.Check.Summary) != "" {
		return e.Check.Summary
	}
	return "gitops repository policy check failed"
}

var loadServiceProjectForGitOpsRepositoryPolicy = loadServiceProject
var resolveServiceScmCredentialForGitOpsRepositoryPolicy = resolveServiceScmCredential
var checkGitOpsRepositoryBaseBranch = func(
	ctx context.Context,
	provider string,
	token string,
	repoURL string,
	baseBranch string,
) error {
	runtime, err := scmproviders.ResolveRuntimeForCapability(provider, scmproviders.CapabilityPullRequests)
	if err != nil {
		return err
	}
	_, err = runtime.LatestCommitSHA(ctx, token, repoURL, baseBranch)
	return err
}

func validateGitOpsRepositoryURL(provider string, repoURL string) error {
	switch scmproviders.Normalize(provider) {
	case "github":
		if _, ok := gh.ParseRepo(repoURL); !ok {
			return errors.New("Repository URL is not a valid GitHub repository.")
		}
		return nil
	default:
		return fmt.Errorf("Repository URL validation is not implemented for SCM provider %q.", strings.TrimSpace(provider))
	}
}

func summarizeGitOpsRepositoryPolicyCheck(check serviceGitOpsRepositoryPolicyCheck) serviceGitOpsRepositoryPolicyCheck {
	invalidCount := 0
	reviewCount := 0
	for _, item := range check.Checks {
		switch item.State {
		case "invalid":
			invalidCount++
		case "needs-review":
			reviewCount++
		}
	}

	switch {
	case invalidCount > 0:
		check.Status = "invalid"
		check.Summary = fmt.Sprintf("GitOps repository policy checks found %d blocking issue(s).", invalidCount)
	case reviewCount > 0:
		check.Status = "needs-review"
		check.Summary = fmt.Sprintf("GitOps repository policy checks need review for %d item(s).", reviewCount)
	default:
		providerLabel := strings.TrimSpace(check.Provider)
		if providerLabel == "" {
			providerLabel = "repository"
		}
		check.Status = "verified"
		check.Summary = fmt.Sprintf("GitOps repository policy checks passed for %s on branch %s.", providerLabel, check.BaseBranch)
	}

	return check
}

func buildServiceGitOpsRepositoryPolicyCheck(
	ctx context.Context,
	service bson.M,
	baseBranchOverride string,
) (serviceGitOpsRepositoryPolicyCheck, error) {
	repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
	baseBranch := defaultGitOpsBaseBranch(service, baseBranchOverride)
	check := serviceGitOpsRepositoryPolicyCheck{
		RepoURL:    repoURL,
		BaseBranch: baseBranch,
		Checks:     make([]serviceGitOpsRepositoryPolicyCheckItem, 0, 5),
	}

	if isObservedService(service) {
		check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
			ID:      "management-mode",
			Label:   "Management mode",
			State:   "invalid",
			Message: "GitOps pull request delivery is only available for managed services.",
		})
		return summarizeGitOpsRepositoryPolicyCheck(check), nil
	}

	if repoURL == "" {
		check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
			ID:      "repository-url",
			Label:   "Repository URL",
			State:   "invalid",
			Message: "Configure a repository URL before using GitOps pull request delivery.",
		})
		return summarizeGitOpsRepositoryPolicyCheck(check), nil
	}

	check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
		ID:      "repository-url",
		Label:   "Repository URL",
		State:   "verified",
		Message: fmt.Sprintf("Repository URL is set to %s.", repoURL),
	})

	project, err := loadServiceProjectForGitOpsRepositoryPolicy(ctx, service)
	if err != nil {
		return serviceGitOpsRepositoryPolicyCheck{}, err
	}

	credential, err := resolveServiceScmCredentialForGitOpsRepositoryPolicy(ctx, service, project)
	if err != nil {
		return serviceGitOpsRepositoryPolicyCheck{}, err
	}
	if credential == nil {
		check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
			ID:      "scm-credential",
			Label:   "SCM credential",
			State:   "invalid",
			Message: "Select a service, project, or platform SCM credential before using GitOps pull requests.",
		})
		return summarizeGitOpsRepositoryPolicyCheck(check), nil
	}

	provider := scmproviders.Normalize(shared.StringValue(credential["provider"]))
	check.Provider = provider
	token := strings.TrimSpace(shared.StringValue(credential["token"]))
	credentialMessage := fmt.Sprintf("Using %s credential for repository automation.", provider)
	credentialState := "verified"
	if token == "" {
		credentialState = "invalid"
		credentialMessage = fmt.Sprintf("%s credential is missing a token.", provider)
	}
	check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
		ID:      "scm-credential",
		Label:   "SCM credential",
		State:   credentialState,
		Message: credentialMessage,
	})

	capabilityState := "verified"
	capabilityMessage := fmt.Sprintf("%s supports pull-request based GitOps delivery.", provider)
	if !scmproviders.SupportsCapability(provider, scmproviders.CapabilityPullRequests) {
		capabilityState = "invalid"
		capabilityMessage = fmt.Sprintf("%s does not support pull-request based GitOps delivery yet.", provider)
	}
	check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
		ID:      "provider-capability",
		Label:   "SCM provider capability",
		State:   capabilityState,
		Message: capabilityMessage,
	})

	repoCompatibilityState := "verified"
	repoCompatibilityMessage := fmt.Sprintf("Repository URL is compatible with %s pull-request delivery.", provider)
	if err := validateGitOpsRepositoryURL(provider, repoURL); err != nil {
		repoCompatibilityState = "invalid"
		repoCompatibilityMessage = err.Error()
	}
	check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
		ID:      "provider-compatibility",
		Label:   "Repository compatibility",
		State:   repoCompatibilityState,
		Message: repoCompatibilityMessage,
	})

	baseBranchState := "needs-review"
	baseBranchMessage := fmt.Sprintf("Resolve repository access blockers before validating branch %s.", baseBranch)
	if credentialState == "verified" && capabilityState == "verified" && repoCompatibilityState == "verified" {
		if err := checkGitOpsRepositoryBaseBranch(ctx, provider, token, repoURL, baseBranch); err != nil {
			baseBranchState = "invalid"
			baseBranchMessage = fmt.Sprintf("Base branch %s cannot be read with the current SCM credential.", baseBranch)
		} else {
			baseBranchState = "verified"
			baseBranchMessage = fmt.Sprintf("Base branch %s is reachable with the current SCM credential.", baseBranch)
		}
	}
	check.Checks = append(check.Checks, serviceGitOpsRepositoryPolicyCheckItem{
		ID:      "base-branch",
		Label:   "Base branch access",
		State:   baseBranchState,
		Message: baseBranchMessage,
	})

	return summarizeGitOpsRepositoryPolicyCheck(check), nil
}

func GetServiceGitOpsRepositoryPolicyCheck(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
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
			"message": "GitOps repository checks are only available for managed services.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return
	}
	if strings.TrimSpace(shared.StringValue(service["repoUrl"])) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service repository URL is required for GitOps repository checks")
		return
	}

	check, err := buildServiceGitOpsRepositoryPolicyCheck(ctx, service, strings.TrimSpace(c.Query("baseBranch")))
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to evaluate GitOps repository policy")
		return
	}

	c.JSON(http.StatusOK, check)
}

func ensureGitOpsRepositoryPolicyReady(ctx context.Context, service bson.M) error {
	check, err := buildServiceGitOpsRepositoryPolicyCheck(ctx, service, "")
	if err != nil {
		return err
	}
	if check.Status != "verified" {
		return serviceGitOpsRepositoryPolicyError{Check: check}
	}
	return nil
}
