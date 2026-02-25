package workers

import (
	"testing"
	"time"
)

func TestNormalizeAutoDeployLeaseTTL(t *testing.T) {
	if got := normalizeAutoDeployLeaseTTL(0); got != defaultAutoDeployLeaseTTLSeconds {
		t.Fatalf("expected default ttl, got %d", got)
	}
	if got := normalizeAutoDeployLeaseTTL(10); got != minAutoDeployLeaseTTLSeconds {
		t.Fatalf("expected min ttl clamp, got %d", got)
	}
	if got := normalizeAutoDeployLeaseTTL(9999); got != maxAutoDeployLeaseTTLSeconds {
		t.Fatalf("expected max ttl clamp, got %d", got)
	}
}

func TestNormalizeAutoDeployLeaseEnvironment(t *testing.T) {
	if got := normalizeAutoDeployLeaseEnvironment("production"); got != "prod" {
		t.Fatalf("expected prod, got %q", got)
	}
	if got := normalizeAutoDeployLeaseEnvironment("sandbox"); got != "dev" {
		t.Fatalf("expected dev, got %q", got)
	}
	if got := normalizeAutoDeployLeaseEnvironment("custom"); got != "custom" {
		t.Fatalf("expected custom passthrough, got %q", got)
	}
}

func TestAutoDeployLeaseStillValid(t *testing.T) {
	now := time.Now().UTC()
	if autoDeployLeaseStillValid(now.Add(-time.Minute).Format(time.RFC3339), now) {
		t.Fatalf("expired lease must be invalid")
	}
	if !autoDeployLeaseStillValid(now.Add(time.Minute).Format(time.RFC3339), now) {
		t.Fatalf("future lease must be valid")
	}
	if autoDeployLeaseStillValid("invalid-date", now) {
		t.Fatalf("invalid timestamp must be treated as invalid")
	}
}
