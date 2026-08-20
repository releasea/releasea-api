package settings

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	platformmodels "releaseaapi/internal/platform/models"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

type scmHealthRuntimeStub struct {
	token string
}

func (runtime *scmHealthRuntimeStub) HealthCheck(_ context.Context, token string) error {
	runtime.token = token
	return nil
}

type registryHealthRuntimeStub struct {
	password string
}

func (runtime *registryHealthRuntimeStub) HealthCheck(_ context.Context, credential map[string]interface{}) error {
	runtime.password = shared.StringValue(credential["password"])
	return nil
}

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

func TestProviderHealthDecryptsStoredCredentials(t *testing.T) {
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")

	encryptedToken, err := shared.EncryptSensitiveValue("github-token")
	if err != nil {
		t.Fatalf("encrypt token: %v", err)
	}
	encryptedPassword, err := shared.EncryptSensitiveValue("registry-password")
	if err != nil {
		t.Fatalf("encrypt password: %v", err)
	}

	scmRuntime := &scmHealthRuntimeStub{}
	registryRuntime := &registryHealthRuntimeStub{}
	previousSCMResolver := resolveSCMHealthRuntime
	previousRegistryResolver := resolveRegistryHealthRuntime
	resolveSCMHealthRuntime = func(string) (scmHealthRuntime, error) { return scmRuntime, nil }
	resolveRegistryHealthRuntime = func(string) (registryHealthRuntime, error) { return registryRuntime, nil }
	t.Cleanup(func() {
		resolveSCMHealthRuntime = previousSCMResolver
		resolveRegistryHealthRuntime = previousRegistryResolver
	})

	catalog := buildProviderCatalog()
	scmChecks := buildSCMHealthChecks(context.Background(), catalog.SCM, []bson.M{{
		"id": "scm-1", "name": "GitHub", "provider": "github", "authType": "token", "token": encryptedToken,
	}})
	registryChecks := buildRegistryHealthChecks(context.Background(), catalog.Registry, []bson.M{{
		"id": "registry-1", "name": "Registry", "provider": "docker", "password": encryptedPassword,
	}})

	if len(scmChecks) != 1 || scmChecks[0].State != providerHealthHealthy {
		t.Fatalf("unexpected SCM health checks: %#v", scmChecks)
	}
	if len(registryChecks) != 1 || registryChecks[0].State != providerHealthHealthy {
		t.Fatalf("unexpected registry health checks: %#v", registryChecks)
	}
	if scmRuntime.token != "github-token" {
		t.Fatalf("SCM runtime received %q", scmRuntime.token)
	}
	if registryRuntime.password != "registry-password" {
		t.Fatalf("registry runtime received %q", registryRuntime.password)
	}
}
