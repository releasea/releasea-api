package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	scmmodels "releaseaapi/internal/features/scm/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestCreateServiceArgoCDGitOpsPullRequestReturnsPullRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousFindRules := findRulesForDesiredState
	previousOpen := openServiceArgoCDStarterPullRequest
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":             "svc-1",
			"name":           "checkout-api",
			"projectId":      "proj-1",
			"managementMode": "managed",
			"repoUrl":        "https://github.com/releasea/checkout-api",
			"branch":         "main",
		}, nil
	}
	findRulesForDesiredState = func(context.Context, string) ([]bson.M, error) {
		return []bson.M{{"id": "rule-1"}}, nil
	}
	openServiceArgoCDStarterPullRequest = func(_ context.Context, service bson.M, rules []bson.M, payload gitOpsArgoCDPullRequestPayload) (*scmmodels.DesiredStatePullRequestResponse, error) {
		if payload.BaseBranch != "release" {
			t.Fatalf("expected baseBranch override to be forwarded, got %q", payload.BaseBranch)
		}
		if sharedName := strings.TrimSpace(service["name"].(string)); sharedName != "checkout-api" {
			t.Fatalf("unexpected service name %q", sharedName)
		}
		if len(rules) != 1 {
			t.Fatalf("expected one rule, got %d", len(rules))
		}
		return &scmmodels.DesiredStatePullRequestResponse{
			URL:        "https://github.com/releasea/checkout-api/pull/21",
			Number:     21,
			BaseBranch: "release",
			BranchName: "releasea/gitops/argocd/checkout-api-20260328123500",
			FilePath:   ".releasea/gitops/checkout-api/desired-state.yaml",
			FilePaths: []string{
				".releasea/gitops/checkout-api/desired-state.yaml",
				".releasea/gitops/checkout-api/kustomization.yaml",
				".releasea/gitops/argocd/checkout-api-application.yaml",
			},
			Title: "chore(gitops): add Argo CD starter for checkout-api",
		}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		findRulesForDesiredState = previousFindRules
		openServiceArgoCDStarterPullRequest = previousOpen
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/services/svc-1/gitops/argocd/pull-requests", strings.NewReader(`{"baseBranch":"release"}`))
	request.Header.Set("Content-Type", "application/json")
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	CreateServiceArgoCDGitOpsPullRequest(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body scmmodels.DesiredStatePullRequestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Number != 21 || len(body.FilePaths) != 3 {
		t.Fatalf("unexpected response payload: %s", recorder.Body.String())
	}
}
