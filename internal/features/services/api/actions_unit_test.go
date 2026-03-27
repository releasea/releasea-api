package services

import (
	"testing"

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
