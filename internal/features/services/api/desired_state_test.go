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
	"go.mongodb.org/mongo-driver/mongo"
)

func TestGetServiceDesiredStateReturnsVersionedExport(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousFindRules := findRulesForDesiredState
	previousNow := nowForDesiredState
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":                   "svc-1",
			"name":                 "Checkout API",
			"projectId":            "proj-1",
			"type":                 "microservice",
			"managementMode":       "managed",
			"sourceType":           "registry",
			"dockerImage":          "ghcr.io/releasea/checkout:1.2.3",
			"dockerContext":        ".",
			"dockerfilePath":       "Dockerfile",
			"deployTemplateId":     "tpl-service",
			"healthCheckPath":      "/ready",
			"port":                 8080,
			"replicas":             2,
			"cpu":                  500,
			"memory":               512,
			"profileId":            "rp-medium",
			"workerTags":           []string{"build", "dev"},
			"registryCredentialId": "reg-1",
			"secretProviderId":     "vault",
			"environment": bson.M{
				"API_URL": "https://api.example.com",
				"API_KEY": "super-secret",
			},
			"deploymentStrategy": bson.M{
				"type": "canary",
			},
			"autoDeploy": true,
			"isActive":   true,
		}, nil
	}
	findRulesForDesiredState = func(context.Context, string) ([]bson.M, error) {
		return []bson.M{
			{
				"id":              "rule-b",
				"name":            "z-public",
				"environment":     "prod",
				"protocol":        "http",
				"port":            80,
				"hosts":           []string{"api.example.com"},
				"paths":           []string{"/"},
				"methods":         []string{"GET"},
				"gateways":        []string{"istio-system/releasea-external-gateway"},
				"status":          "published",
				"lastPublishedAt": "2026-03-28T10:20:00Z",
				"policy": bson.M{
					"action": "allow",
				},
			},
			{
				"id":          "rule-a",
				"name":        "a-internal",
				"environment": "dev",
				"protocol":    "http",
				"port":        8080,
				"hosts":       []string{"checkout.dev.internal"},
				"paths":       []string{"/internal"},
				"methods":     []string{"GET"},
				"gateways":    []string{"istio-system/releasea-internal-gateway"},
				"status":      "draft",
			},
		}, nil
	}
	nowForDesiredState = func() string { return "2026-03-28T12:00:00Z" }
	defer func() {
		findServiceForDesiredState = previousFindService
		findRulesForDesiredState = previousFindRules
		nowForDesiredState = previousNow
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/desired-state", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceDesiredState(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body struct {
		Filename   string   `json:"filename"`
		YAML       string   `json:"yaml"`
		Warnings   []string `json:"warnings"`
		Validation struct {
			Status string `json:"status"`
		} `json:"validation"`
		Document struct {
			Kind       string `json:"kind"`
			APIVersion string `json:"apiVersion"`
			Version    int    `json:"version"`
			Service    struct {
				Spec struct {
					Environment struct {
						Keys []string `json:"keys"`
					} `json:"environment"`
				} `json:"spec"`
			} `json:"service"`
			Rules []struct {
				ID          string `json:"id"`
				Environment string `json:"environment"`
				Publication struct {
					Internal bool `json:"internal"`
					External bool `json:"external"`
				} `json:"publication"`
			} `json:"rules"`
		} `json:"document"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}

	if body.Document.Kind != "releasea.service.desired-state" {
		t.Fatalf("kind = %q, want releasea.service.desired-state", body.Document.Kind)
	}
	if body.Document.APIVersion != "v1" {
		t.Fatalf("apiVersion = %q, want v1", body.Document.APIVersion)
	}
	if body.Document.Version != 1 {
		t.Fatalf("version = %d, want 1", body.Document.Version)
	}
	if body.Filename != "releasea-service-checkout-api-desired-state.yaml" {
		t.Fatalf("filename = %q", body.Filename)
	}
	if len(body.Warnings) != 1 {
		t.Fatalf("warnings = %#v, want exactly one warning", body.Warnings)
	}
	if body.Validation.Status != "verified" {
		t.Fatalf("validation status = %q, want verified", body.Validation.Status)
	}
	if got, want := strings.Join(body.Document.Service.Spec.Environment.Keys, ","), "API_KEY,API_URL"; got != want {
		t.Fatalf("environment keys = %q, want %q", got, want)
	}
	if len(body.Document.Rules) != 2 || body.Document.Rules[0].ID != "rule-a" || body.Document.Rules[1].ID != "rule-b" {
		t.Fatalf("rules should be sorted by environment and name: %#v", body.Document.Rules)
	}
	if !body.Document.Rules[0].Publication.Internal || body.Document.Rules[0].Publication.External {
		t.Fatalf("rule-a publication parsed incorrectly: %#v", body.Document.Rules[0].Publication)
	}
	if body.Document.Rules[1].Publication.Internal || !body.Document.Rules[1].Publication.External {
		t.Fatalf("rule-b publication parsed incorrectly: %#v", body.Document.Rules[1].Publication)
	}
	if !strings.Contains(body.YAML, "kind: releasea.service.desired-state") {
		t.Fatalf("yaml output should include document kind: %s", body.YAML)
	}
	if strings.Contains(body.YAML, "super-secret") {
		t.Fatalf("yaml output should not include raw environment values: %s", body.YAML)
	}
}

func TestGetServiceDesiredStateValidationReturnsIssues(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	previousFindRules := findRulesForDesiredState
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":             "svc-2",
			"name":           "broken-service",
			"projectId":      "proj-1",
			"type":           "microservice",
			"managementMode": "managed",
			"sourceType":     "git",
			"repoUrl":        "",
			"port":           0,
		}, nil
	}
	findRulesForDesiredState = func(context.Context, string) ([]bson.M, error) {
		return []bson.M{{"id": "rule-1", "name": "public", "environment": "prod", "gateways": []string{"external"}}}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
		findRulesForDesiredState = previousFindRules
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/services/svc-2/desired-state/validation", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-2"}}

	GetServiceDesiredStateValidation(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body struct {
		Status string `json:"status"`
		Issues []struct {
			Code string `json:"code"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Status != "invalid" {
		t.Fatalf("validation status = %q, want invalid", body.Status)
	}
	if len(body.Issues) == 0 {
		t.Fatalf("expected validation issues, got %#v", body)
	}
}

func TestGetServiceDesiredStateReturnsConflictForObservedService(t *testing.T) {
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
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/desired-state", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceDesiredState(ctx)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
	if !strings.Contains(recorder.Body.String(), "SERVICE_OBSERVED_MODE") {
		t.Fatalf("response should contain SERVICE_OBSERVED_MODE: %s", recorder.Body.String())
	}
}

func TestGetServiceDesiredStateReturnsNotFound(t *testing.T) {
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
	request := httptest.NewRequest(http.MethodGet, "/services/svc-1/desired-state", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceDesiredState(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
