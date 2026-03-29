package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"

	scmproviders "releaseaapi/internal/platform/providers/scm"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/yaml.v3"
)

type serviceGitOpsDriftStatus struct {
	State        string `json:"state"`
	InSync       bool   `json:"inSync"`
	Message      string `json:"message"`
	RepoURL      string `json:"repoUrl"`
	BaseBranch   string `json:"baseBranch"`
	FilePath     string `json:"filePath"`
	ExpectedHash string `json:"expectedHash"`
	ActualHash   string `json:"actualHash,omitempty"`
}

var recordServiceGitOpsDriftStatusChange = func(ctx context.Context, service bson.M, drift serviceGitOpsDriftStatus) {
	serviceID := strings.TrimSpace(shared.StringValue(service["id"]))
	if serviceID == "" {
		return
	}

	items, err := shared.FindAllSorted(
		ctx,
		shared.Collection(shared.PlatformAuditCollection),
		bson.M{
			"resourceType": "service",
			"resourceId":   serviceID,
			"action":       "service.gitops_drift.state_changed",
		},
		bson.D{{Key: "createdAt", Value: -1}},
	)
	if err == nil && len(items) > 0 {
		metadata := shared.MapPayload(items[0]["metadata"])
		if shared.StringValue(metadata["state"]) == drift.State &&
			shared.StringValue(metadata["expectedHash"]) == drift.ExpectedHash &&
			shared.StringValue(metadata["actualHash"]) == drift.ActualHash &&
			shared.StringValue(metadata["filePath"]) == drift.FilePath {
			return
		}
	}

	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "service.gitops_drift.state_changed",
		ResourceType: "service",
		ResourceID:   serviceID,
		Status:       drift.State,
		Source:       "gitops",
		Message:      drift.Message,
		Metadata: map[string]interface{}{
			"name":         shared.StringValue(service["name"]),
			"state":        drift.State,
			"repoUrl":      drift.RepoURL,
			"baseBranch":   drift.BaseBranch,
			"filePath":     drift.FilePath,
			"expectedHash": drift.ExpectedHash,
			"actualHash":   drift.ActualHash,
		},
	})
}

var checkServiceDesiredStateDrift = func(
	ctx context.Context,
	service bson.M,
	rules []bson.M,
	baseBranch string,
	filePath string,
) (serviceGitOpsDriftStatus, error) {
	project, err := loadServiceProject(ctx, service)
	if err != nil {
		return serviceGitOpsDriftStatus{}, err
	}

	credential, err := resolveServiceScmCredential(ctx, service, project)
	if err != nil {
		return serviceGitOpsDriftStatus{}, err
	}
	if credential == nil {
		return serviceGitOpsDriftStatus{}, errors.New("SCM credential not found")
	}

	token := strings.TrimSpace(shared.StringValue(credential["token"]))
	if token == "" {
		return serviceGitOpsDriftStatus{}, errors.New("SCM credential missing token")
	}

	provider := strings.ToLower(strings.TrimSpace(shared.StringValue(credential["provider"])))
	runtime, err := scmproviders.ResolveRuntimeForCapability(provider, scmproviders.CapabilityRepoFiles)
	if err != nil {
		return serviceGitOpsDriftStatus{}, err
	}

	document, _ := buildServiceDesiredStateDocument(service, rules, nowForDesiredState())
	rendered, err := yaml.Marshal(document)
	if err != nil {
		return serviceGitOpsDriftStatus{}, err
	}

	repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
	expected := normalizeGitOpsContent(string(rendered))
	expectedHash := hashGitOpsContent(expected)
	actual, err := runtime.ReadFileContent(ctx, token, repoURL, filePath, baseBranch)
	if err != nil {
		if errors.Is(err, scmproviders.ErrFileNotFound) {
			return serviceGitOpsDriftStatus{
				State:        "missing",
				InSync:       false,
				Message:      "No desired state file exists in the repository yet.",
				RepoURL:      repoURL,
				BaseBranch:   baseBranch,
				FilePath:     filePath,
				ExpectedHash: expectedHash,
			}, nil
		}
		return serviceGitOpsDriftStatus{}, err
	}

	actualNormalized := normalizeGitOpsContent(actual)
	actualHash := hashGitOpsContent(actualNormalized)
	if actualNormalized == expected {
		return serviceGitOpsDriftStatus{
			State:        "in-sync",
			InSync:       true,
			Message:      "Repository desired state matches the current Releasea export.",
			RepoURL:      repoURL,
			BaseBranch:   baseBranch,
			FilePath:     filePath,
			ExpectedHash: expectedHash,
			ActualHash:   actualHash,
		}, nil
	}

	return serviceGitOpsDriftStatus{
		State:        "out-of-sync",
		InSync:       false,
		Message:      "Repository desired state is out of sync with the current Releasea export.",
		RepoURL:      repoURL,
		BaseBranch:   baseBranch,
		FilePath:     filePath,
		ExpectedHash: expectedHash,
		ActualHash:   actualHash,
	}, nil
}

func GetServiceGitOpsDrift(c *gin.Context) {
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
			"message": "GitOps drift checks are only available for managed services.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return
	}
	if strings.TrimSpace(shared.StringValue(service["repoUrl"])) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service repository URL is required for GitOps drift checks")
		return
	}

	rules, err := findRulesForDesiredState(ctx, serviceID)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service rules")
		return
	}

	baseBranch := defaultGitOpsBaseBranch(service, c.Query("baseBranch"))
	driftPaths := resolveServiceGitOpsDriftPaths(service, c.Query("filePath"))
	if len(driftPaths) == 0 {
		shared.RespondError(c, http.StatusInternalServerError, "No GitOps layout paths are available for drift checks")
		return
	}

	drift, err := checkServiceDesiredStateDrift(ctx, service, rules, baseBranch, driftPaths[0])
	if err == nil && len(driftPaths) > 1 && drift.State == "missing" {
		for _, path := range driftPaths[1:] {
			drift, err = checkServiceDesiredStateDrift(ctx, service, rules, baseBranch, path)
			if err != nil || drift.State != "missing" {
				break
			}
		}
	}
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}
	recordServiceGitOpsDriftStatusChange(ctx, service, drift)
	c.JSON(http.StatusOK, drift)
}

func normalizeGitOpsContent(content string) string {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.TrimSpace(normalized)
	if normalized == "" {
		return ""
	}
	return normalized + "\n"
}

func hashGitOpsContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
