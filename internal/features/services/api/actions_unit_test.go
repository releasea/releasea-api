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

func TestResolveServiceDeployStrategyType(t *testing.T) {
	tests := []struct {
		name    string
		service bson.M
		expect  string
	}{
		{
			name: "cronjob template forces rolling",
			service: bson.M{
				"deployTemplateId": "tpl-cronjob",
				"deploymentStrategy": bson.M{
					"type": "canary",
				},
			},
			expect: "rolling",
		},
		{
			name: "canary strategy",
			service: bson.M{
				"deploymentStrategy": bson.M{
					"type": " canary ",
				},
			},
			expect: "canary",
		},
		{
			name: "blue-green strategy",
			service: bson.M{
				"deploymentStrategy": bson.M{
					"type": "blue-green",
				},
			},
			expect: "blue-green",
		},
		{
			name: "invalid strategy defaults rolling",
			service: bson.M{
				"deploymentStrategy": bson.M{
					"type": "custom",
				},
			},
			expect: "rolling",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveServiceDeployStrategyType(tt.service)
			if got != tt.expect {
				t.Fatalf("resolveServiceDeployStrategyType() = %q, want %q", got, tt.expect)
			}
		})
	}
}

func TestBuildDeployStrategyStatus(t *testing.T) {
	tests := []struct {
		name    string
		service bson.M
		expect  bson.M
	}{
		{
			name: "canary clamps percent",
			service: bson.M{
				"deploymentStrategy": bson.M{
					"type":          "canary",
					"canaryPercent": 75,
				},
			},
			expect: bson.M{
				"exposurePercent": 50,
				"stablePercent":   50,
			},
		},
		{
			name: "blue-green defaults primary blue",
			service: bson.M{
				"deploymentStrategy": bson.M{
					"type":             "blue-green",
					"blueGreenPrimary": "invalid",
				},
			},
			expect: bson.M{
				"activeSlot":   "blue",
				"inactiveSlot": "green",
			},
		},
		{
			name: "rolling uses min replicas fallback",
			service: bson.M{
				"deploymentStrategy": bson.M{
					"type": "rolling",
				},
				"replicas":    0,
				"minReplicas": 3,
			},
			expect: bson.M{
				"targetReplicas": 3,
			},
		},
		{
			name: "rolling defaults to one replica",
			service: bson.M{
				"deploymentStrategy": bson.M{
					"type": "rolling",
				},
			},
			expect: bson.M{
				"targetReplicas": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := buildDeployStrategyStatus(tt.service, "phase", "summary", "now")
			details := status["details"].(bson.M)
			for key, expectValue := range tt.expect {
				if got := details[key]; got != expectValue {
					t.Fatalf("details[%q] = %v, want %v", key, got, expectValue)
				}
			}
		})
	}
}

func TestNormalizeServiceSourceType(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{input: "registry", expect: "registry"},
		{input: "docker", expect: "registry"},
		{input: "git", expect: "git"},
		{input: "unknown", expect: ""},
	}

	for _, tt := range tests {
		got := normalizeServiceSourceType(tt.input)
		if got != tt.expect {
			t.Fatalf("normalizeServiceSourceType(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestIsRegistrySourceType(t *testing.T) {
	if !isRegistrySourceType("docker") {
		t.Fatalf("docker should be treated as registry")
	}
	if isRegistrySourceType("git") {
		t.Fatalf("git should not be treated as registry")
	}
}

func TestNormalizeServiceManagementMode(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{input: "", expect: "managed"},
		{input: "managed", expect: "managed"},
		{input: " observed ", expect: "observed"},
		{input: "custom", expect: ""},
	}

	for _, tt := range tests {
		got := normalizeServiceManagementMode(tt.input)
		if got != tt.expect {
			t.Fatalf("normalizeServiceManagementMode(%q) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestIsObservedService(t *testing.T) {
	if !isObservedService(bson.M{"managementMode": "observed"}) {
		t.Fatalf("observed service should be detected")
	}
	if isObservedService(bson.M{"managementMode": "managed"}) {
		t.Fatalf("managed service should not be detected as observed")
	}
	if isObservedService(bson.M{}) {
		t.Fatalf("missing management mode should default to managed")
	}
}

func TestDetectObservedRestrictedMutationFields(t *testing.T) {
	existing := bson.M{
		"managementMode":     "observed",
		"sourceType":         "registry",
		"dockerImage":        "ghcr.io/acme/payments:1.0.0",
		"deploymentStrategy": bson.M{"type": "rolling"},
		"environment": bson.M{
			"LOG_LEVEL": "info",
		},
	}

	blocked := detectObservedRestrictedMutationFields(
		existing,
		bson.M{
			"managementMode":     "observed",
			"dockerImage":        "ghcr.io/acme/payments:2.0.0",
			"deployStrategyType": "canary",
			"canaryPercent":      10,
			"environment": bson.M{
				"LOG_LEVEL": "debug",
			},
		},
		true,
		normalizedServiceStrategy{Type: "canary", CanaryPercent: 10},
		false,
	)

	if len(blocked) != 3 {
		t.Fatalf("blocked fields = %v, want 3 entries", blocked)
	}
}

func TestDetectObservedRestrictedMutationFieldsAllowsManagedTransition(t *testing.T) {
	existing := bson.M{
		"managementMode": "observed",
		"sourceType":     "registry",
		"dockerImage":    "ghcr.io/acme/payments:1.0.0",
	}

	blocked := detectObservedRestrictedMutationFields(
		existing,
		bson.M{
			"managementMode": "managed",
			"dockerImage":    "ghcr.io/acme/payments:2.0.0",
		},
		false,
		normalizedServiceStrategy{},
		false,
	)

	if len(blocked) != 0 {
		t.Fatalf("blocked fields = %v, want none when switching back to managed", blocked)
	}
}

func TestRespondIfObservedServiceDeleteBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	blocked := respondIfObservedServiceDeleteBlocked(ctx, bson.M{"managementMode": "observed"})
	if !blocked {
		t.Fatalf("expected observed service deletion to be blocked")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestVersionAliasAndImageReferenceHelpers(t *testing.T) {
	if !isDeployVersionAlias("") {
		t.Fatalf("empty version should be alias")
	}
	if !isDeployVersionAlias("latest") {
		t.Fatalf("latest should be alias")
	}
	if !isDeployVersionAlias("repo/image:latest") {
		t.Fatalf("image with :latest should be alias")
	}
	if isDeployVersionAlias("v1.2.3") {
		t.Fatalf("fixed version should not be alias")
	}

	if got := withImageTag("repo/image:old", "v2"); got != "repo/image:v2" {
		t.Fatalf("withImageTag(tagged) = %q, want %q", got, "repo/image:v2")
	}
	if got := withImageTag("repo/image@sha256:abcd", "v2"); got != "repo/image:v2" {
		t.Fatalf("withImageTag(digest) = %q, want %q", got, "repo/image:v2")
	}
	if got := withImageTag("", "v2"); got != "" {
		t.Fatalf("withImageTag(empty image) = %q, want empty", got)
	}

	if got := extractImageTagOrDigest("repo/image:v3"); got != "v3" {
		t.Fatalf("extractImageTagOrDigest(tag) = %q, want %q", got, "v3")
	}
	if got := extractImageTagOrDigest("repo/image@sha256:abcd"); got != "sha256:abcd" {
		t.Fatalf("extractImageTagOrDigest(digest) = %q, want %q", got, "sha256:abcd")
	}
	if got := extractImageTagOrDigest("repo/image"); got != "" {
		t.Fatalf("extractImageTagOrDigest(no tag) = %q, want empty", got)
	}

	if !isImageReference("repo/image:v1") {
		t.Fatalf("tagged image should be image reference")
	}
	if !isImageReference("repo/image@sha256:abcd") {
		t.Fatalf("digest image should be image reference")
	}
	if isImageReference("v1") {
		t.Fatalf("plain version should not be image reference")
	}
}

func TestResolvePolicyRegistryHostFromImage(t *testing.T) {
	tests := []struct {
		image  string
		expect string
	}{
		{image: "ghcr.io/releasea/api:v1", expect: "ghcr.io"},
		{image: "localhost:5000/releasea/api:v1", expect: "localhost:5000"},
		{image: "releasea/api:v1", expect: "docker.io"},
		{image: "", expect: ""},
	}

	for _, tt := range tests {
		if got := resolvePolicyRegistryHostFromImage(tt.image); got != tt.expect {
			t.Fatalf("resolvePolicyRegistryHostFromImage(%q) = %q, want %q", tt.image, got, tt.expect)
		}
	}
}

func TestResolveServiceDeployReplicaTarget(t *testing.T) {
	tests := []struct {
		name    string
		service bson.M
		expect  int
	}{
		{
			name: "uses replicas first",
			service: bson.M{
				"replicas":    4,
				"minReplicas": 2,
			},
			expect: 4,
		},
		{
			name: "falls back to min replicas",
			service: bson.M{
				"replicas":    0,
				"minReplicas": 2,
			},
			expect: 2,
		},
		{
			name:    "defaults to one",
			service: bson.M{},
			expect:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveServiceDeployReplicaTarget(tt.service); got != tt.expect {
				t.Fatalf("resolveServiceDeployReplicaTarget() = %d, want %d", got, tt.expect)
			}
		})
	}
}

func TestMaybeRespondDeployPolicyBlockedReturnsConflictJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := loadGovernanceSettings
	previousAudit := recordGovernancePolicyBlockAudit
	loadGovernanceSettings = func(context.Context) (bson.M, error) {
		return bson.M{
			"deployPolicy": bson.M{
				"enabled": true,
				"rules": []interface{}{
					bson.M{
						"environment":            "prod",
						"allowAutoDeploy":        false,
						"requireExplicitVersion": true,
						"allowedSourceTypes":     []string{"registry"},
						"allowedRegistries":      []string{"ghcr.io"},
						"allowedStrategies":      []string{"rolling"},
						"maxReplicas":            2,
					},
				},
			},
		}, nil
	}
	recordGovernancePolicyBlockAudit = func(context.Context, string, string, bson.M, bson.M) {}
	defer func() {
		loadGovernanceSettings = previous
		recordGovernancePolicyBlockAudit = previousAudit
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	blocked := maybeRespondDeployPolicyBlocked(ctx, context.Background(), "svc-1", "Checkout API", bson.M{}, "prod", "auto", "git", "docker.io", "canary", 4, false)
	if !blocked {
		t.Fatalf("expected deploy policy to block request")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	var body struct {
		Code       string `json:"code"`
		Message    string `json:"message"`
		Queued     bool   `json:"queued"`
		Violations []struct {
			Code string `json:"code"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Code != "GOVERNANCE_DEPLOY_POLICY_VIOLATION" {
		t.Fatalf("code = %q, want %q", body.Code, "GOVERNANCE_DEPLOY_POLICY_VIOLATION")
	}
	if body.Queued {
		t.Fatalf("queued should be false for blocked request")
	}
	if len(body.Violations) != 6 {
		t.Fatalf("violations = %d, want %d", len(body.Violations), 6)
	}
}

func TestMaybeRespondDeployPolicyBlockedAllowsCompliantRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previous := loadGovernanceSettings
	previousAudit := recordGovernancePolicyBlockAudit
	loadGovernanceSettings = func(context.Context) (bson.M, error) {
		return bson.M{
			"deployPolicy": bson.M{
				"enabled": true,
				"rules": []interface{}{
					bson.M{
						"environment":            "prod",
						"allowAutoDeploy":        true,
						"requireExplicitVersion": true,
						"allowedSourceTypes":     []string{"registry"},
						"allowedRegistries":      []string{"ghcr.io"},
						"allowedStrategies":      []string{"rolling", "blue-green"},
						"maxReplicas":            4,
					},
				},
			},
		}, nil
	}
	recordGovernancePolicyBlockAudit = func(context.Context, string, string, bson.M, bson.M) {}
	defer func() {
		loadGovernanceSettings = previous
		recordGovernancePolicyBlockAudit = previousAudit
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	blocked := maybeRespondDeployPolicyBlocked(ctx, context.Background(), "svc-1", "Checkout API", bson.M{}, "prod", "manual", "registry", "ghcr.io", "rolling", 3, true)
	if blocked {
		t.Fatalf("expected compliant deploy request to pass policy check")
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("expected no response body for allowed request, got %q", recorder.Body.String())
	}
}
