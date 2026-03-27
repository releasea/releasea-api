package settings

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	platformmodels "releaseaapi/internal/platform/models"

	"github.com/gin-gonic/gin"
)

func TestBuildProviderCatalogDefaults(t *testing.T) {
	catalog := buildProviderCatalog()

	if catalog.Version != "1" {
		t.Fatalf("version = %q, want %q", catalog.Version, "1")
	}
	if catalog.SCM.DefaultProvider != "github" {
		t.Fatalf("scm default = %q, want %q", catalog.SCM.DefaultProvider, "github")
	}
	if catalog.Registry.DefaultProvider != "docker" {
		t.Fatalf("registry default = %q, want %q", catalog.Registry.DefaultProvider, "docker")
	}
	if catalog.Secrets.DefaultProvider != "vault" {
		t.Fatalf("secrets default = %q, want %q", catalog.Secrets.DefaultProvider, "vault")
	}
	if len(catalog.Identity.Providers) != 2 {
		t.Fatalf("identity providers = %d, want %d", len(catalog.Identity.Providers), 2)
	}
}

func TestGetProviderCatalogReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	GetProviderCatalog(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body platformmodels.ProviderCatalog
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	if body.Notifications.Kind != "notifications" {
		t.Fatalf("notifications kind = %q, want %q", body.Notifications.Kind, "notifications")
	}
	if len(body.SCM.Providers) < 3 {
		t.Fatalf("scm providers = %d, want at least %d", len(body.SCM.Providers), 3)
	}
}
