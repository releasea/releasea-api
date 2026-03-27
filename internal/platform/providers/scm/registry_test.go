package scmproviders

import "testing"

func TestValidateCredential(t *testing.T) {
	if err := ValidateCredential("github", "token"); err != nil {
		t.Fatalf("github token should be valid: %v", err)
	}
	if err := ValidateCredential("gitlab", "ssh"); err != nil {
		t.Fatalf("gitlab ssh should be valid: %v", err)
	}
	if err := ValidateCredential("unknown", "token"); err == nil {
		t.Fatalf("expected unsupported provider validation error")
	}
	if err := ValidateCredential("github", "basic"); err == nil {
		t.Fatalf("expected unsupported auth mode validation error")
	}
}

func TestSupportsCapability(t *testing.T) {
	if !SupportsCapability("github", CapabilityTemplateRepo) {
		t.Fatalf("github should support template repo capability")
	}
	if SupportsCapability("gitlab", CapabilityTemplateRepo) {
		t.Fatalf("gitlab should not support template repo capability")
	}
}

func TestResolveRuntimeForCapability(t *testing.T) {
	runtime, err := ResolveRuntimeForCapability("github", CapabilityCommitLookup)
	if err != nil {
		t.Fatalf("expected github runtime for commit lookup: %v", err)
	}
	if runtime.ID() != "github" {
		t.Fatalf("expected github runtime, got %q", runtime.ID())
	}

	if _, err := ResolveRuntimeForCapability("gitlab", CapabilityTemplateRepo); err == nil {
		t.Fatalf("expected capability validation error for gitlab template repo operations")
	}
	if _, err := ResolveRuntimeForCapability("unknown", CapabilityCommitLookup); err == nil {
		t.Fatalf("expected unsupported provider error")
	}
}
