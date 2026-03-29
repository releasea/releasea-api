package governance

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestCreateGovernanceExceptionCreatesDeployPolicyException(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForGovernanceException
	previousInsert := insertGovernanceException
	previousAudit := recordGovernanceExceptionAudit
	findServiceForGovernanceException = func(context.Context, string) (bson.M, error) {
		return bson.M{"id": "svc-1", "name": "Checkout API"}, nil
	}
	var inserted bson.M
	insertGovernanceException = func(_ context.Context, doc bson.M) error {
		inserted = doc
		return nil
	}
	recordGovernanceExceptionAudit = func(context.Context, string, string, string, string, bson.M, map[string]interface{}) {}
	defer func() {
		findServiceForGovernanceException = previousFindService
		insertGovernanceException = previousInsert
		recordGovernanceExceptionAudit = previousAudit
	}()

	payload := map[string]interface{}{
		"policy":      "deploy-policy",
		"serviceId":   "svc-1",
		"environment": "prod",
		"codes":       []string{"explicit-version-required"},
		"reason":      "Planned migration",
		"expiresAt":   time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("authRole", "admin")
	ctx.Set("authUserId", "usr-admin")
	ctx.Set("authEmail", "admin@releasea.io")
	ctx.Set("authName", "Admin User")
	ctx.Request = httptest.NewRequest(http.MethodPost, "/governance/exceptions", bytes.NewReader(body))
	ctx.Request.Header.Set("Content-Type", "application/json")

	CreateGovernanceException(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d: %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if inserted["policy"] != shared.GovernanceExceptionPolicyDeploy {
		t.Fatalf("expected deploy-policy to be inserted, got %v", inserted["policy"])
	}
	if inserted["serviceName"] != "Checkout API" {
		t.Fatalf("expected service name to be resolved, got %v", inserted["serviceName"])
	}
}

func TestGetGovernanceExceptionsNormalizesStatuses(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFind := findGovernanceExceptions
	findGovernanceExceptions = func(context.Context, bson.M) ([]bson.M, error) {
		return []bson.M{
			{
				"id":          "gexc-active",
				"policy":      "deploy-policy",
				"serviceId":   "svc-1",
				"serviceName": "Checkout API",
				"environment": "prod",
				"codes":       []string{"*"},
				"reason":      "Migration",
				"expiresAt":   time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
				"createdAt":   time.Now().UTC().Format(time.RFC3339),
			},
			{
				"id":          "gexc-expired",
				"policy":      "deploy-policy",
				"serviceId":   "svc-2",
				"serviceName": "Billing API",
				"environment": "prod",
				"codes":       []string{"registry-not-allowed"},
				"reason":      "Cleanup",
				"expiresAt":   time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
				"createdAt":   time.Now().UTC().Add(-2 * time.Hour).Format(time.RFC3339),
			},
		}, nil
	}
	defer func() {
		findGovernanceExceptions = previousFind
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("authRole", "admin")
	ctx.Request = httptest.NewRequest(http.MethodGet, "/governance/exceptions", nil)

	GetGovernanceExceptions(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body []governanceTemporaryExceptionResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 exceptions, got %d", len(body))
	}
	if body[0].Status != "active" {
		t.Fatalf("expected active exception first, got %q", body[0].Status)
	}
	if body[1].Status != "expired" {
		t.Fatalf("expected expired exception second, got %q", body[1].Status)
	}
}
