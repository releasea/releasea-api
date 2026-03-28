package governance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestGetGovernanceAuditMergesPlatformAndGovernanceEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindGovernanceAuditEntries := findGovernanceAuditEntries
	previousFindPlatformAuditEntries := findPlatformAuditEntries
	findGovernanceAuditEntries = func(context.Context) ([]bson.M, error) {
		return []bson.M{
			{
				"id":           "gaudit-1",
				"action":       "governance.settings.updated",
				"resourceType": "settings",
				"resourceId":   "governance-settings",
				"resourceName": "Governance Settings",
				"performedBy": bson.M{
					"id":    "usr-admin",
					"name":  "Admin User",
					"email": "admin@releasea.io",
				},
				"performedAt": "2026-03-28T10:00:00Z",
				"details": bson.M{
					"section": "deployPolicy",
				},
			},
		}, nil
	}
	findPlatformAuditEntries = func(context.Context) ([]bson.M, error) {
		return []bson.M{
			{
				"id":           "audit-1",
				"action":       "deploy.queued",
				"resourceType": "deploy",
				"resourceId":   "deploy-1",
				"status":       "accepted",
				"source":       "api",
				"createdAt":    "2026-03-28T11:00:00Z",
				"actor": bson.M{
					"id":   "usr-dev",
					"name": "Developer User",
					"role": "developer",
				},
				"metadata": bson.M{
					"name":        "Checkout API",
					"serviceId":   "svc-1",
					"environment": "prod",
					"operationId": "op-1",
				},
			},
		}, nil
	}
	defer func() {
		findGovernanceAuditEntries = previousFindGovernanceAuditEntries
		findPlatformAuditEntries = previousFindPlatformAuditEntries
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("authRole", "admin")
	request := httptest.NewRequest(http.MethodGet, "/governance/audit", nil)
	ctx.Request = request

	GetGovernanceAudit(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body []map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if len(body) != 2 {
		t.Fatalf("expected 2 audit entries, got %d: %s", len(body), recorder.Body.String())
	}
	if body[0]["id"] != "audit-1" {
		t.Fatalf("expected platform audit entry to be sorted first, got %v", body[0]["id"])
	}
	if body[0]["resourceName"] != "Checkout API" {
		t.Fatalf("expected platform audit resource name from metadata, got %v", body[0]["resourceName"])
	}
	performedBy, ok := body[0]["performedBy"].(map[string]interface{})
	if !ok {
		t.Fatalf("performedBy should be an object: %T", body[0]["performedBy"])
	}
	if performedBy["name"] != "Developer User" {
		t.Fatalf("expected platform actor to be normalized, got %v", performedBy["name"])
	}
	details, ok := body[0]["details"].(map[string]interface{})
	if !ok {
		t.Fatalf("details should be an object: %T", body[0]["details"])
	}
	if details["status"] != "accepted" {
		t.Fatalf("expected platform audit status in details, got %v", details["status"])
	}
}
