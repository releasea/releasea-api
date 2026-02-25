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
