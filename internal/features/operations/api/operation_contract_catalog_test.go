package operations

import (
	"slices"
	"strings"
	"testing"
)

func TestOperationContractCatalogVersion(t *testing.T) {
	if OperationContractCatalogVersion != "v1" {
		t.Fatalf("operation contract version = %q, want v1", OperationContractCatalogVersion)
	}
}

func TestSupportedOperationTypes(t *testing.T) {
	got := SupportedOperationTypes()
	want := []string{
		OperationTypeServiceDeploy,
		OperationTypeServicePromoteCanary,
		OperationTypeServiceDelete,
		OperationTypeRuleDeploy,
		OperationTypeRulePublish,
		OperationTypeRuleDelete,
		OperationTypeWorkerRestart,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("supported operation types = %v, want %v", got, want)
	}
}

func TestOperationContractCatalogCompleteness(t *testing.T) {
	statuses := SupportedOperationStatuses()
	catalog := OperationContractCatalog()
	for _, operationType := range SupportedOperationTypes() {
		spec, ok := catalog[operationType]
		if !ok {
			t.Fatalf("catalog missing operation type %s", operationType)
		}
		if spec.Type != operationType {
			t.Fatalf("catalog type mismatch for %s: %s", operationType, spec.Type)
		}
		if spec.Version != OperationContractCatalogVersion {
			t.Fatalf("catalog version mismatch for %s: %s", operationType, spec.Version)
		}
		if !slices.Equal(spec.StatusLifecycle, statuses) {
			t.Fatalf("status lifecycle mismatch for %s: %v", operationType, spec.StatusLifecycle)
		}
		if spec.ResourceType == "" {
			t.Fatalf("resource type missing for %s", operationType)
		}
		if len(spec.PayloadRequired) == 0 {
			t.Fatalf("required payload fields missing for %s", operationType)
		}
		if spec.RetryPolicy == "" {
			t.Fatalf("retry policy missing for %s", operationType)
		}
		if spec.RollbackPolicy == "" {
			t.Fatalf("rollback policy missing for %s", operationType)
		}
		if spec.CompatibilityRule == "" {
			t.Fatalf("compatibility rule missing for %s", operationType)
		}
	}
}

func TestOperationContractByType(t *testing.T) {
	spec, ok := OperationContractByType(OperationTypeRulePublish)
	if !ok {
		t.Fatalf("expected %s to exist in contract catalog", OperationTypeRulePublish)
	}
	if spec.HandlerAliasOf != OperationTypeRuleDeploy {
		t.Fatalf("rule publish alias = %q, want %q", spec.HandlerAliasOf, OperationTypeRuleDeploy)
	}
	if IsSupportedOperationType("unknown.operation") {
		t.Fatalf("unknown operation type should not be supported")
	}
}

func TestRuleOperationPoliciesExposeRetryAndRollbackContract(t *testing.T) {
	for _, operationType := range []string{OperationTypeRuleDeploy, OperationTypeRulePublish} {
		spec, ok := OperationContractByType(operationType)
		if !ok {
			t.Fatalf("expected %s to exist in contract catalog", operationType)
		}
		if !strings.Contains(strings.ToLower(spec.RetryPolicy), "retry") {
			t.Fatalf("expected retry policy for %s, got %q", operationType, spec.RetryPolicy)
		}
		if !strings.Contains(strings.ToLower(spec.RollbackPolicy), "revert") {
			t.Fatalf("expected rollback policy for %s, got %q", operationType, spec.RollbackPolicy)
		}
	}
}
