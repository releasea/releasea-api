package services

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestGetServiceGitOpsRepositoryPolicyCheckReturnsVerified(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousLoadProject := loadServiceProjectForGitOpsRepositoryPolicy
	previousResolveCredential := resolveServiceScmCredentialForGitOpsRepositoryPolicy
	previousCheckBranch := checkGitOpsRepositoryBaseBranch
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":             "svc-1",
			"name":           "checkout-api",
			"projectId":      "proj-1",
			"managementMode": "managed",
			"repoUrl":        "https://github.com/releasea/checkout-api",
			"branch":         "release",
		}, nil
	}
	loadServiceProjectForGitOpsRepositoryPolicy = func(context.Context, bson.M) (bson.M, error) {
		return bson.M{"id": "proj-1"}, nil
	}
	resolveServiceScmCredentialForGitOpsRepositoryPolicy = func(context.Context, bson.M, bson.M) (bson.M, error) {
		return bson.M{"provider": "github", "token": "ghp_test"}, nil
	}
	checkGitOpsRepositoryBaseBranch = func(_ context.Context, provider, token, repoURL, baseBranch string) error {
		if provider != "github" || token != "ghp_test" || repoURL != "https://github.com/releasea/checkout-api" || baseBranch != "release" {
			t.Fatalf("unexpected branch check payload: provider=%s token=%s repo=%s branch=%s", provider, token, repoURL, baseBranch)
		}
		return nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		loadServiceProjectForGitOpsRepositoryPolicy = previousLoadProject
		resolveServiceScmCredentialForGitOpsRepositoryPolicy = previousResolveCredential
		checkGitOpsRepositoryBaseBranch = previousCheckBranch
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/gitops/repository-policy-check", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceGitOpsRepositoryPolicyCheck(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body serviceGitOpsRepositoryPolicyCheck
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Status != "verified" {
		t.Fatalf("status = %q, want verified", body.Status)
	}
	if body.BaseBranch != "release" {
		t.Fatalf("baseBranch = %q, want release", body.BaseBranch)
	}
	if len(body.Checks) != 5 {
		t.Fatalf("checks = %d, want 5", len(body.Checks))
	}
}

func TestGetServiceGitOpsRepositoryPolicyCheckReturnsInvalidWhenProviderLacksPRCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousLoadProject := loadServiceProjectForGitOpsRepositoryPolicy
	previousResolveCredential := resolveServiceScmCredentialForGitOpsRepositoryPolicy
	previousCheckBranch := checkGitOpsRepositoryBaseBranch
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
	loadServiceProjectForGitOpsRepositoryPolicy = func(context.Context, bson.M) (bson.M, error) {
		return bson.M{"id": "proj-1"}, nil
	}
	resolveServiceScmCredentialForGitOpsRepositoryPolicy = func(context.Context, bson.M, bson.M) (bson.M, error) {
		return bson.M{"provider": "gitlab", "token": "glpat_test"}, nil
	}
	checkGitOpsRepositoryBaseBranch = func(context.Context, string, string, string, string) error {
		t.Fatal("base branch check should not run when provider capability is invalid")
		return nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		loadServiceProjectForGitOpsRepositoryPolicy = previousLoadProject
		resolveServiceScmCredentialForGitOpsRepositoryPolicy = previousResolveCredential
		checkGitOpsRepositoryBaseBranch = previousCheckBranch
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/gitops/repository-policy-check", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceGitOpsRepositoryPolicyCheck(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body serviceGitOpsRepositoryPolicyCheck
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Status != "invalid" {
		t.Fatalf("status = %q, want invalid", body.Status)
	}
	if body.Provider != "gitlab" {
		t.Fatalf("provider = %q, want gitlab", body.Provider)
	}
}

func TestCreateServiceGitOpsPullRequestRejectsInvalidRepositoryPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousLoadProject := loadServiceProjectForGitOpsRepositoryPolicy
	previousResolveCredential := resolveServiceScmCredentialForGitOpsRepositoryPolicy
	previousCheckBranch := checkGitOpsRepositoryBaseBranch
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
	loadServiceProjectForGitOpsRepositoryPolicy = func(context.Context, bson.M) (bson.M, error) {
		return bson.M{"id": "proj-1"}, nil
	}
	resolveServiceScmCredentialForGitOpsRepositoryPolicy = func(context.Context, bson.M, bson.M) (bson.M, error) {
		return bson.M{"provider": "github", "token": "ghp_test"}, nil
	}
	checkGitOpsRepositoryBaseBranch = func(context.Context, string, string, string, string) error {
		return errors.New("branch not found")
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		loadServiceProjectForGitOpsRepositoryPolicy = previousLoadProject
		resolveServiceScmCredentialForGitOpsRepositoryPolicy = previousResolveCredential
		checkGitOpsRepositoryBaseBranch = previousCheckBranch
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
	if !strings.Contains(recorder.Body.String(), "GITOPS_REPOSITORY_POLICY_INVALID") {
		t.Fatalf("response should contain GITOPS_REPOSITORY_POLICY_INVALID: %s", recorder.Body.String())
	}
}
