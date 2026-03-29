package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestGetServiceGitOpsDriftReturnsDriftStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousFindRules := findRulesForDesiredState
	previousCheck := checkServiceDesiredStateDrift
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":             "svc-1",
			"name":           "checkout-api",
			"managementMode": "managed",
			"repoUrl":        "https://github.com/releasea/checkout-api",
			"branch":         "main",
		}, nil
	}
	findRulesForDesiredState = func(context.Context, string) ([]bson.M, error) {
		return []bson.M{{"id": "rule-1"}}, nil
	}
	checkServiceDesiredStateDrift = func(_ context.Context, service bson.M, rules []bson.M, baseBranch string, filePath string) (serviceGitOpsDriftStatus, error) {
		if baseBranch != "main" {
			t.Fatalf("expected default base branch main, got %q", baseBranch)
		}
		if filePath != ".releasea/gitops/checkout-api.desired-state.yaml" {
			t.Fatalf("unexpected file path %q", filePath)
		}
		if len(rules) != 1 {
			t.Fatalf("expected one rule, got %d", len(rules))
		}
		return serviceGitOpsDriftStatus{
			State:      "out-of-sync",
			InSync:     false,
			Message:    "Repository desired state is out of sync with the current Releasea export.",
			RepoURL:    strings.TrimSpace(service["repoUrl"].(string)),
			BaseBranch: baseBranch,
			FilePath:   filePath,
		}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		findRulesForDesiredState = previousFindRules
		checkServiceDesiredStateDrift = previousCheck
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/gitops/drift", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceGitOpsDrift(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body serviceGitOpsDriftStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.State != "out-of-sync" || body.InSync {
		t.Fatalf("unexpected drift response: %s", recorder.Body.String())
	}
}

func TestGetServiceGitOpsDriftFallsBackToArgoCDStarterPath(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousFindRules := findRulesForDesiredState
	previousCheck := checkServiceDesiredStateDrift
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":             "svc-1",
			"name":           "checkout-api",
			"managementMode": "managed",
			"repoUrl":        "https://github.com/releasea/checkout-api",
			"branch":         "main",
		}, nil
	}
	findRulesForDesiredState = func(context.Context, string) ([]bson.M, error) {
		return []bson.M{{"id": "rule-1"}}, nil
	}
	callCount := 0
	checkServiceDesiredStateDrift = func(_ context.Context, service bson.M, rules []bson.M, baseBranch string, filePath string) (serviceGitOpsDriftStatus, error) {
		callCount++
		if callCount == 1 {
			return serviceGitOpsDriftStatus{
				State:      "missing",
				InSync:     false,
				Message:    "No desired state file exists in the repository yet.",
				RepoURL:    strings.TrimSpace(service["repoUrl"].(string)),
				BaseBranch: baseBranch,
				FilePath:   filePath,
			}, nil
		}
		if filePath != ".releasea/gitops/checkout-api/desired-state.yaml" {
			t.Fatalf("unexpected fallback file path %q", filePath)
		}
		return serviceGitOpsDriftStatus{
			State:      "in-sync",
			InSync:     true,
			Message:    "Repository desired state matches the current Releasea export.",
			RepoURL:    strings.TrimSpace(service["repoUrl"].(string)),
			BaseBranch: baseBranch,
			FilePath:   filePath,
		}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		findRulesForDesiredState = previousFindRules
		checkServiceDesiredStateDrift = previousCheck
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/gitops/drift", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceGitOpsDrift(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if callCount != 2 {
		t.Fatalf("expected two drift checks, got %d", callCount)
	}

	var body serviceGitOpsDriftStatus
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.State != "in-sync" || !body.InSync {
		t.Fatalf("unexpected drift response: %s", recorder.Body.String())
	}
}

func TestResolveServiceGitOpsDriftPathsIncludesUniquePresetPaths(t *testing.T) {
	service := bson.M{
		"name":    "checkout-api",
		"repoUrl": "https://github.com/releasea/checkout-api",
	}

	paths := resolveServiceGitOpsDriftPaths(service, "")
	if len(paths) != 2 {
		t.Fatalf("paths = %d, want 2", len(paths))
	}
	if paths[0] != ".releasea/gitops/checkout-api.desired-state.yaml" {
		t.Fatalf("first path = %q", paths[0])
	}
	if paths[1] != ".releasea/gitops/checkout-api/desired-state.yaml" {
		t.Fatalf("second path = %q", paths[1])
	}
}

func TestNormalizeGitOpsContent(t *testing.T) {
	input := "kind: test\r\nvalue: yes\r\n\r\n"
	got := normalizeGitOpsContent(input)
	want := "kind: test\nvalue: yes\n"
	if got != want {
		t.Fatalf("normalizeGitOpsContent() = %q, want %q", got, want)
	}
}
