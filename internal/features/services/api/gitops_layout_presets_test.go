package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestGetServiceGitOpsLayoutPresetsReturnsAvailablePresets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindService := findServiceForDesiredState
	findServiceForDesiredState = func(context.Context, string) (bson.M, error) {
		return bson.M{
			"id":             "svc-1",
			"name":           "checkout-api",
			"managementMode": "managed",
			"repoUrl":        "https://github.com/releasea/checkout-api",
		}, nil
	}
	defer func() {
		findServiceForDesiredState = previousFindService
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/services/svc-1/gitops/layout-presets", nil)
	ctx.Params = gin.Params{{Key: "id", Value: "svc-1"}}

	GetServiceGitOpsLayoutPresets(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body []serviceGitOpsLayoutPreset
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if len(body) != 3 {
		t.Fatalf("presets = %d, want 3", len(body))
	}
	if body[0].ID != "legacy" || body[1].ID != "argocd" || body[2].ID != "flux" {
		t.Fatalf("unexpected preset order: %+v", body)
	}
	if !body[0].Available || !body[1].Available || !body[2].Available {
		t.Fatalf("all presets should be available for a managed service with repo url: %+v", body)
	}
}

func TestBuildServiceGitOpsLayoutPresetsMarksObservedServicesUnavailable(t *testing.T) {
	presets := buildServiceGitOpsLayoutPresets(bson.M{
		"name":           "checkout-api",
		"managementMode": "observed",
		"repoUrl":        "https://github.com/releasea/checkout-api",
	})

	if len(presets) != 3 {
		t.Fatalf("presets = %d, want 3", len(presets))
	}
	if presets[0].Available {
		t.Fatalf("legacy preset should be unavailable for observed service")
	}
	if presets[0].AvailabilityReason == "" {
		t.Fatalf("expected availability reason for observed service")
	}
}
