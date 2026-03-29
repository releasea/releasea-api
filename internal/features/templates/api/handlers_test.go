package templates

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestVerifyTemplatesReturnsVerificationForArrayPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)

	body, err := json.Marshal([]map[string]any{
		{
			"id":          "tpl-ok",
			"label":       "API",
			"type":        "microservice",
			"repoMode":    "template",
			"description": "api starter",
			"category":    "Services",
			"owner":       "releasea",
			"bestFor":     "APIs",
			"setupTime":   "5 min",
			"tier":        "core",
			"templateSource": map[string]any{
				"owner": "releasea",
				"repo":  "templates",
				"path":  "api",
			},
			"templateDefaults": map[string]any{
				"port":            "8080",
				"healthCheckPath": "/healthz",
			},
		},
		{
			"id":   "tpl-bad",
			"type": "microservice",
		},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/templates/verify", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = req

	VerifyTemplates(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response) != 2 {
		t.Fatalf("response len = %d, want 2", len(response))
	}
	firstVerification, _ := response[0]["verification"].(map[string]any)
	if firstVerification["status"] != "verified" {
		t.Fatalf("first verification status = %v, want verified", firstVerification["status"])
	}
	secondVerification, _ := response[1]["verification"].(map[string]any)
	if secondVerification["status"] != "invalid" {
		t.Fatalf("second verification status = %v, want invalid", secondVerification["status"])
	}
}
