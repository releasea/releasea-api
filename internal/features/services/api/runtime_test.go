package services

import "testing"

func TestNormalizeRuntimeEnvironment(t *testing.T) {
	if got := normalizeRuntimeEnvironment("production"); got != "prod" {
		t.Fatalf("expected prod, got %q", got)
	}
	if got := normalizeRuntimeEnvironment("sandbox"); got != "dev" {
		t.Fatalf("expected dev, got %q", got)
	}
	if got := normalizeRuntimeEnvironment("custom"); got != "custom" {
		t.Fatalf("expected custom passthrough, got %q", got)
	}
}

func TestMapRuntimeToServiceStatus(t *testing.T) {
	if got := mapRuntimeToServiceStatus("healthy"); got != "running" {
		t.Fatalf("expected running for healthy, got %q", got)
	}
	if got := mapRuntimeToServiceStatus("idle"); got != "idle" {
		t.Fatalf("expected idle for idle, got %q", got)
	}
	if got := mapRuntimeToServiceStatus("degraded"); got != "error" {
		t.Fatalf("expected error for degraded, got %q", got)
	}
	if got := mapRuntimeToServiceStatus("unknown-value"); got != "" {
		t.Fatalf("expected empty mapping for unknown status, got %q", got)
	}
}

func TestShouldUpdateServiceStatus(t *testing.T) {
	if shouldUpdateServiceStatus("running", false, "error") {
		t.Fatalf("inactive service should not be updated")
	}
	if shouldUpdateServiceStatus("creating", true, "running") {
		t.Fatalf("creating service should not be updated")
	}
	if !shouldUpdateServiceStatus("running", true, "error") {
		t.Fatalf("active service transition should be allowed")
	}
	if shouldUpdateServiceStatus("running", true, "running") {
		t.Fatalf("same status should not trigger update")
	}
}
