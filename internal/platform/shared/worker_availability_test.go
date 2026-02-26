package shared

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestNormalizeOperationEnvironment(t *testing.T) {
	if got := NormalizeOperationEnvironment(""); got != "prod" {
		t.Fatalf("empty environment = %q, want %q", got, "prod")
	}
	if got := NormalizeOperationEnvironment("  staging "); got != "staging" {
		t.Fatalf("trimmed environment = %q, want %q", got, "staging")
	}
}

func TestWorkerUnavailableMessage(t *testing.T) {
	if got := WorkerUnavailableMessage(""); got != "No active worker available for prod environment" {
		t.Fatalf("unexpected message for default env: %q", got)
	}
	if got := WorkerUnavailableMessage(" dev "); got != "No active worker available for dev environment" {
		t.Fatalf("unexpected message for provided env: %q", got)
	}
}

func TestWorkerStaleSeconds(t *testing.T) {
	t.Setenv("WORKER_STALE_SECONDS", "")
	if got := workerStaleSeconds(); got != 90 {
		t.Fatalf("default stale seconds = %d, want %d", got, 90)
	}

	t.Setenv("WORKER_STALE_SECONDS", "120")
	if got := workerStaleSeconds(); got != 120 {
		t.Fatalf("parsed stale seconds = %d, want %d", got, 120)
	}

	t.Setenv("WORKER_STALE_SECONDS", "0")
	if got := workerStaleSeconds(); got != 90 {
		t.Fatalf("non-positive stale seconds should fallback, got %d", got)
	}

	t.Setenv("WORKER_STALE_SECONDS", "invalid")
	if got := workerStaleSeconds(); got != 90 {
		t.Fatalf("invalid stale seconds should fallback, got %d", got)
	}
}

func TestIsWorkerHeartbeatFresh(t *testing.T) {
	threshold := time.Date(2026, time.January, 1, 10, 0, 0, 0, time.UTC)

	if !isWorkerHeartbeatFresh(bson.M{
		"lastHeartbeat": threshold.Format(time.RFC3339),
	}, threshold) {
		t.Fatalf("heartbeat equal to threshold should be fresh")
	}

	if isWorkerHeartbeatFresh(bson.M{
		"lastHeartbeat": threshold.Add(-time.Second).Format(time.RFC3339),
	}, threshold) {
		t.Fatalf("heartbeat older than threshold should not be fresh")
	}

	if isWorkerHeartbeatFresh(bson.M{
		"lastHeartbeat": "not-a-date",
	}, threshold) {
		t.Fatalf("invalid heartbeat format should not be fresh")
	}

	if isWorkerHeartbeatFresh(bson.M{}, threshold) {
		t.Fatalf("missing heartbeat should not be fresh")
	}
}
