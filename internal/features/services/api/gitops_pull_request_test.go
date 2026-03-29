package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	scmmodels "releaseaapi/internal/features/scm/models"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestCreateServiceGitOpsPullRequestReturnsPullRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousFindRules := findRulesForDesiredState
	previousOpen := openServiceDesiredStatePullRequest
	previousNow := nowForGitOpsPullRequest
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
	nowForGitOpsPullRequest = func() time.Time {
		return time.Date(2026, 3, 28, 12, 34, 56, 0, time.UTC)
	}
	openServiceDesiredStatePullRequest = func(_ context.Context, service bson.M, rules []bson.M, payload gitOpsPullRequestPayload) (*scmmodels.DesiredStatePullRequestResponse, error) {
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
			URL:        "https://github.com/releasea/checkout-api/pull/17",
			Number:     17,
			BaseBranch: "release",
			BranchName: "releasea/gitops/checkout-api-20260328123456",
			FilePath:   ".releasea/gitops/checkout-api.desired-state.yaml",
			Title:      "chore(gitops): update desired state for checkout-api",
		}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		findRulesForDesiredState = previousFindRules
		openServiceDesiredStatePullRequest = previousOpen
		nowForGitOpsPullRequest = previousNow
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/services/svc-1/gitops/pull-requests", strings.NewReader(`{"baseBranch":"release"}`))
	request.Header.Set("Content-Type", "application/json")
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	CreateServiceGitOpsPullRequest(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body scmmodels.DesiredStatePullRequestResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Number != 17 || body.URL == "" {
		t.Fatalf("unexpected response payload: %s", recorder.Body.String())
	}
}

func TestCreateServiceGitOpsPullRequestRejectsObservedServices(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{"id": "svc-1", "managementMode": "observed"}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/services/svc-1/gitops/pull-requests", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	CreateServiceGitOpsPullRequest(ctx)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if !strings.Contains(recorder.Body.String(), "SERVICE_OBSERVED_MODE") {
		t.Fatalf("response should contain SERVICE_OBSERVED_MODE: %s", recorder.Body.String())
	}
}

func TestCreateServiceGitOpsPullRequestReturnsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return nil, mongo.ErrNoDocuments
	}
	defer func() {
		findServiceForDesiredState = previousFindService
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/services/svc-1/gitops/pull-requests", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	CreateServiceGitOpsPullRequest(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}

func TestCreateServiceGitOpsPullRequestRejectsInvalidDesiredState(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousFindRules := findRulesForDesiredState
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":             "svc-1",
			"name":           "checkout-api",
			"projectId":      "proj-1",
			"type":           "microservice",
			"managementMode": "managed",
			"sourceType":     "git",
			"repoUrl":        "https://github.com/releasea/checkout-api",
			"branch":         "main",
			"port":           0,
		}, nil
	}
	findRulesForDesiredState = func(context.Context, string) ([]bson.M, error) {
		return []bson.M{}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		findRulesForDesiredState = previousFindRules
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "/services/svc-1/gitops/pull-requests", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	CreateServiceGitOpsPullRequest(ctx)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if !strings.Contains(recorder.Body.String(), "GITOPS_DESIRED_STATE_INVALID") {
		t.Fatalf("response should contain GITOPS_DESIRED_STATE_INVALID: %s", recorder.Body.String())
	}
}
