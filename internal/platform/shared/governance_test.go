package shared

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestNormalizeGovernanceApprovalType(t *testing.T) {
	if got := NormalizeGovernanceApprovalType(" deploy "); got != GovernanceApprovalTypeDeploy {
		t.Fatalf("NormalizeGovernanceApprovalType(deploy) = %q", got)
	}
	if got := NormalizeGovernanceApprovalType("rule-publish"); got != GovernanceApprovalTypeRulePublish {
		t.Fatalf("NormalizeGovernanceApprovalType(rule-publish) = %q", got)
	}
	if got := NormalizeGovernanceApprovalType("invalid"); got != "" {
		t.Fatalf("NormalizeGovernanceApprovalType(invalid) should be empty, got %q", got)
	}
}

func TestNormalizeGovernanceApprovalStatus(t *testing.T) {
	if got := NormalizeGovernanceApprovalStatus(" approved "); got != GovernanceApprovalStatusApproved {
		t.Fatalf("NormalizeGovernanceApprovalStatus(approved) = %q", got)
	}
	if got := NormalizeGovernanceApprovalStatus("rejected"); got != GovernanceApprovalStatusRejected {
		t.Fatalf("NormalizeGovernanceApprovalStatus(rejected) = %q", got)
	}
	if got := NormalizeGovernanceApprovalStatus("pending"); got != GovernanceApprovalStatusPending {
		t.Fatalf("NormalizeGovernanceApprovalStatus(pending) = %q", got)
	}
	if got := NormalizeGovernanceApprovalStatus("unknown"); got != "" {
		t.Fatalf("NormalizeGovernanceApprovalStatus(unknown) should be empty, got %q", got)
	}
}

func TestDeployApprovalRequired(t *testing.T) {
	tests := []struct {
		name          string
		settings      bson.M
		environment   string
		expectNeeded  bool
		expectMinimum int
	}{
		{
			name: "disabled returns false",
			settings: bson.M{
				"deployApproval": bson.M{"enabled": false},
			},
			environment:   "prod",
			expectNeeded:  false,
			expectMinimum: 1,
		},
		{
			name: "enabled with explicit prod",
			settings: bson.M{
				"deployApproval": bson.M{
					"enabled":      true,
					"environments": []string{"prod"},
					"minApprovers": 2,
				},
			},
			environment:   "prod",
			expectNeeded:  true,
			expectMinimum: 2,
		},
		{
			name: "enabled but environment not listed",
			settings: bson.M{
				"deployApproval": bson.M{
					"enabled":      true,
					"environments": []string{"prod"},
					"minApprovers": 2,
				},
			},
			environment:   "dev",
			expectNeeded:  false,
			expectMinimum: 2,
		},
		{
			name: "empty environment list defaults to prod only",
			settings: bson.M{
				"deployApproval": bson.M{
					"enabled":      true,
					"environments": []string{},
					"minApprovers": 0,
				},
			},
			environment:   "prod",
			expectNeeded:  true,
			expectMinimum: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needed, min := DeployApprovalRequired(tt.settings, tt.environment)
			if needed != tt.expectNeeded || min != tt.expectMinimum {
				t.Fatalf("DeployApprovalRequired() = (%v,%d), want (%v,%d)", needed, min, tt.expectNeeded, tt.expectMinimum)
			}
		})
	}
}

func TestRulePublishApprovalRequired(t *testing.T) {
	tests := []struct {
		name          string
		settings      bson.M
		external      bool
		expectNeeded  bool
		expectMinimum int
	}{
		{
			name: "disabled",
			settings: bson.M{
				"rulePublishApproval": bson.M{"enabled": false},
			},
			external:      true,
			expectNeeded:  false,
			expectMinimum: 1,
		},
		{
			name: "external only blocks internal",
			settings: bson.M{
				"rulePublishApproval": bson.M{
					"enabled":      true,
					"externalOnly": true,
					"minApprovers": 3,
				},
			},
			external:      false,
			expectNeeded:  false,
			expectMinimum: 1,
		},
		{
			name: "external allowed",
			settings: bson.M{
				"rulePublishApproval": bson.M{
					"enabled":      true,
					"externalOnly": true,
					"minApprovers": 3,
				},
			},
			external:      true,
			expectNeeded:  true,
			expectMinimum: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			needed, min := RulePublishApprovalRequired(tt.settings, tt.external)
			if needed != tt.expectNeeded || min != tt.expectMinimum {
				t.Fatalf("RulePublishApprovalRequired() = (%v,%d), want (%v,%d)", needed, min, tt.expectNeeded, tt.expectMinimum)
			}
		})
	}
}

func TestMinApproversForApprovalType(t *testing.T) {
	settings := bson.M{
		"deployApproval": bson.M{
			"minApprovers": 2,
		},
		"rulePublishApproval": bson.M{
			"minApprovers": 4,
		},
	}

	if got := MinApproversForApprovalType(settings, GovernanceApprovalTypeDeploy); got != 2 {
		t.Fatalf("deploy min approvers = %d, want 2", got)
	}
	if got := MinApproversForApprovalType(settings, GovernanceApprovalTypeRulePublish); got != 4 {
		t.Fatalf("rule-publish min approvers = %d, want 4", got)
	}
	if got := MinApproversForApprovalType(settings, "invalid"); got != 1 {
		t.Fatalf("invalid type min approvers = %d, want 1", got)
	}
}

func TestNormalizeDeployPolicyRules(t *testing.T) {
	rules := NormalizeDeployPolicyRules([]interface{}{
		bson.M{
			"environment":              " prod ",
			"allowAutoDeploy":          false,
			"requireExplicitVersion":   true,
			"blockExternalExposure":    true,
			"allowedProfileIds":        []string{" rp-medium ", "rp-medium", "RP-LARGE"},
			"allowedScmProviders":      []string{" github ", "gitlab", "GitHub"},
			"allowedRegistryProviders": []string{" ghcr ", "docker", "GHCR"},
			"allowedSecretProviders":   []string{" vault ", "gcp", "VAULT"},
			"allowedSourceTypes":       []string{" git ", "registry", "git", "invalid"},
			"allowedRegistries":        []string{" ghcr.io ", "https://ghcr.io", "index.docker.io", "invalid/host"},
			"allowedStrategies":        []string{" rolling ", "canary", "rolling", "invalid"},
			"maxReplicas":              5,
		},
		bson.M{
			"environment":       "dev",
			"allowAutoDeploy":   true,
			"allowedStrategies": []string{"blue-green"},
			"maxReplicas":       -1,
		},
	})

	if len(rules) != 2 {
		t.Fatalf("expected 2 normalized rules, got %d", len(rules))
	}
	if got := rules[1]["environment"]; got != "prod" {
		t.Fatalf("expected prod rule to be normalized, got %v", got)
	}
	if got := rules[1]["allowedStrategies"]; len(got.([]string)) != 2 {
		t.Fatalf("expected deduplicated strategies, got %v", got)
	}
	if got := rules[1]["requireExplicitVersion"]; got != true {
		t.Fatalf("expected requireExplicitVersion to be normalized, got %v", got)
	}
	if got := rules[1]["blockExternalExposure"]; got != true {
		t.Fatalf("expected blockExternalExposure to be normalized, got %v", got)
	}
	if got := rules[1]["allowedSourceTypes"]; len(got.([]string)) != 2 {
		t.Fatalf("expected normalized allowedSourceTypes, got %v", got)
	}
	if got := rules[1]["allowedRegistries"]; len(got.([]string)) != 2 {
		t.Fatalf("expected normalized allowedRegistries, got %v", got)
	}
	if got := rules[1]["allowedProfileIds"]; len(got.([]string)) != 2 {
		t.Fatalf("expected normalized allowedProfileIds, got %v", got)
	}
	if got := rules[1]["allowedScmProviders"]; len(got.([]string)) != 2 {
		t.Fatalf("expected normalized allowedScmProviders, got %v", got)
	}
	if got := rules[1]["allowedRegistryProviders"]; len(got.([]string)) != 2 {
		t.Fatalf("expected normalized allowedRegistryProviders, got %v", got)
	}
	if got := rules[1]["allowedSecretProviders"]; len(got.([]string)) != 2 {
		t.Fatalf("expected normalized allowedSecretProviders, got %v", got)
	}
	if got := rules[0]["maxReplicas"]; got != 0 {
		t.Fatalf("expected negative maxReplicas to normalize to 0, got %v", got)
	}
}

func TestEvaluateExternalExposurePolicy(t *testing.T) {
	settings := bson.M{
		"deployPolicy": bson.M{
			"enabled": true,
			"rules": []interface{}{
				bson.M{
					"environment":           "prod",
					"blockExternalExposure": true,
				},
			},
		},
	}

	violations := EvaluateExternalExposurePolicy(settings, "prod", true)
	if len(violations) != 1 {
		t.Fatalf("expected 1 exposure policy violation, got %d", len(violations))
	}
	if violations[0].Code != "external-exposure-disabled" {
		t.Fatalf("expected external-exposure-disabled code, got %q", violations[0].Code)
	}

	if got := EvaluateExternalExposurePolicy(settings, "prod", false); len(got) != 0 {
		t.Fatalf("expected no exposure violations for internal-only publish, got %v", got)
	}
}

func TestEvaluateDeployPolicy(t *testing.T) {
	settings := bson.M{
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
					"maxReplicas":            3,
				},
			},
		},
	}

	violations := EvaluateDeployPolicy(settings, "prod", "auto", "canary", "git", "docker.io", 5, false, GovernanceDeployPolicyTarget{})
	if len(violations) != 6 {
		t.Fatalf("expected 6 policy violations, got %d", len(violations))
	}
	if violations[0].Environment != "prod" {
		t.Fatalf("expected prod environment, got %q", violations[0].Environment)
	}
	if violations[0].Code == "" {
		t.Fatalf("expected violation code")
	}

	if got := EvaluateDeployPolicy(settings, "dev", "manual", "rolling", "registry", "ghcr.io", 1, true, GovernanceDeployPolicyTarget{}); len(got) != 0 {
		t.Fatalf("expected no violations for unmatched environment, got %v", got)
	}
}

func TestEvaluateDeployPolicyAllowsWhitelistedRegistry(t *testing.T) {
	settings := bson.M{
		"deployPolicy": bson.M{
			"enabled": true,
			"rules": []interface{}{
				bson.M{
					"environment":       "prod",
					"allowedRegistries": []string{"ghcr.io", "docker.io"},
				},
			},
		},
	}

	if got := EvaluateDeployPolicy(settings, "prod", "manual", "rolling", "registry", "ghcr.io", 1, true, GovernanceDeployPolicyTarget{}); len(got) != 0 {
		t.Fatalf("expected allowed registry to pass, got %v", got)
	}

	violations := EvaluateDeployPolicy(settings, "prod", "manual", "rolling", "registry", "", 1, true, GovernanceDeployPolicyTarget{})
	if len(violations) != 1 || violations[0].Code != "registry-host-unresolved" {
		t.Fatalf("expected unresolved registry host violation, got %v", violations)
	}
}

func TestEvaluateDeployPolicyChecksProfileAndProviders(t *testing.T) {
	settings := bson.M{
		"deployPolicy": bson.M{
			"enabled": true,
			"rules": []interface{}{
				bson.M{
					"environment":              "prod",
					"allowedProfileIds":        []string{"rp-medium"},
					"allowedScmProviders":      []string{"github"},
					"allowedRegistryProviders": []string{"ghcr"},
					"allowedSecretProviders":   []string{"vault"},
				},
			},
		},
	}

	violations := EvaluateDeployPolicy(settings, "prod", "manual", "rolling", "git", "ghcr.io", 1, true, GovernanceDeployPolicyTarget{
		ProfileID:        "rp-large",
		SCMProvider:      "gitlab",
		RegistryProvider: "docker",
		SecretProvider:   "gcp",
	})
	if len(violations) != 3 {
		t.Fatalf("expected 3 provider/profile violations, got %d", len(violations))
	}
	if violations[0].Code == "" {
		t.Fatalf("expected violation codes to be populated")
	}
}
