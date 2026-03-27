package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	platformmodels "releaseaapi/internal/platform/models"

	"github.com/gin-gonic/gin"
)

func TestGetProviderHealthReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/v1/settings/providers/health", GetProviderHealth)

	previous := providerHealthLoader
	providerHealthLoader = func(context.Context) (platformmodels.ProviderHealthCatalog, error) {
		return platformmodels.ProviderHealthCatalog{
			Version: "1",
			SCM: platformmodels.ProviderHealthCategory{
				Kind:      "scm",
				Healthy:   1,
				Unhealthy: 0,
				Checks: []platformmodels.ProviderHealthCheck{
					{
						ProviderID:    "github",
						ProviderLabel: "GitHub",
						ResourceID:    "scm-1",
						ResourceLabel: "Platform GitHub",
						State:         "healthy",
						Message:       "Credential validated successfully",
					},
				},
			},
			Registry:      platformmodels.ProviderHealthCategory{Kind: "registry", Checks: []platformmodels.ProviderHealthCheck{}},
			Secrets:       platformmodels.ProviderHealthCategory{Kind: "secrets", Checks: []platformmodels.ProviderHealthCheck{}},
			Identity:      platformmodels.ProviderHealthCategory{Kind: "identity", Checks: []platformmodels.ProviderHealthCheck{}},
			Notifications: platformmodels.ProviderHealthCategory{Kind: "notifications", Checks: []platformmodels.ProviderHealthCheck{}},
		}, nil
	}
	defer func() {
		providerHealthLoader = previous
	}()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/settings/providers/health", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body platformmodels.ProviderHealthCatalog
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	if body.SCM.Kind != "scm" {
		t.Fatalf("scm kind = %q, want %q", body.SCM.Kind, "scm")
	}
	if len(body.SCM.Checks) != 1 {
		t.Fatalf("scm checks = %d, want %d", len(body.SCM.Checks), 1)
	}
	if body.SCM.Checks[0].State != providerHealthHealthy {
		t.Fatalf("scm check state = %q, want %q", body.SCM.Checks[0].State, providerHealthHealthy)
	}
}
