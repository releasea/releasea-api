package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestGetServiceDeployPolicyCheckReturnsEvaluation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDeployPolicyCheck
	previousEvaluate := evaluateServiceDeployPolicyCheck
	findServiceForDeployPolicyCheck = func(context.Context, string) (bson.M, error) {
		return bson.M{"id": "svc-1", "name": "Checkout API"}, nil
	}
	evaluateServiceDeployPolicyCheck = func(context.Context, bson.M, string, string) (deployPolicyEvaluationResult, error) {
		return deployPolicyEvaluationResult{
			Environment:     "prod",
			Trigger:         "manual",
			SourceType:      "registry",
			RegistryHost:    "ghcr.io",
			StrategyType:    "rolling",
			Replicas:        3,
			ExplicitVersion: false,
			Target: shared.GovernanceDeployPolicyTarget{
				ProfileID:        "rp-medium",
				RegistryProvider: "ghcr",
			},
			Violations: []shared.GovernanceDeployPolicyViolation{
				{
					Code:        "explicit-version-required",
					Environment: "prod",
					Message:     "Version pinning is required.",
				},
			},
		}, nil
	}
	defer func() {
		findServiceForDeployPolicyCheck = previousFindService
		evaluateServiceDeployPolicyCheck = previousEvaluate
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/deploy-policy-check?environment=prod", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceDeployPolicyCheck(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body struct {
		Environment string `json:"environment"`
		Violations  []struct {
			Code string `json:"code"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Environment != "prod" {
		t.Fatalf("environment = %q, want %q", body.Environment, "prod")
	}
	if len(body.Violations) != 1 || body.Violations[0].Code != "explicit-version-required" {
		t.Fatalf("unexpected violations payload: %s", recorder.Body.String())
	}
}

func TestGetServiceDeployPolicyCheckReturnsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDeployPolicyCheck
	findServiceForDeployPolicyCheck = func(context.Context, string) (bson.M, error) {
		return nil, mongo.ErrNoDocuments
	}
	defer func() {
		findServiceForDeployPolicyCheck = previousFindService
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/deploy-policy-check?environment=prod", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceDeployPolicyCheck(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
