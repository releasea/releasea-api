package operations

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestNormalizeOperationClaimLeaseTTL(t *testing.T) {
	if got := normalizeOperationClaimLeaseTTL(0); got != defaultOperationClaimLeaseTTLSeconds {
		t.Fatalf("ttl default = %d, want %d", got, defaultOperationClaimLeaseTTLSeconds)
	}
	if got := normalizeOperationClaimLeaseTTL(5); got != minOperationClaimLeaseTTLSeconds {
		t.Fatalf("ttl min clamp = %d, want %d", got, minOperationClaimLeaseTTLSeconds)
	}
	if got := normalizeOperationClaimLeaseTTL(9999); got != maxOperationClaimLeaseTTLSeconds {
		t.Fatalf("ttl max clamp = %d, want %d", got, maxOperationClaimLeaseTTLSeconds)
	}
}

func TestBuildOperationClaimMetadata(t *testing.T) {
	now := time.Date(2026, time.March, 29, 20, 0, 0, 0, time.UTC)
	metadata := buildOperationClaimMetadata(
		bson.M{"id": "reg-1", "name": "worker-dev-a", "workerId": "worker-dev-a"},
		"reg-1",
		&operationClaimStatusPayload{TTLSeconds: 180, QueueName: "releasea.worker"},
		now,
	)

	if got := metadata["holder"]; got != "worker-dev-a" {
		t.Fatalf("holder = %v, want %q", got, "worker-dev-a")
	}
	if got := metadata["workerRegistrationId"]; got != "reg-1" {
		t.Fatalf("workerRegistrationId = %v, want %q", got, "reg-1")
	}
	if got := metadata["workerId"]; got != "worker-dev-a" {
		t.Fatalf("workerId = %v, want %q", got, "worker-dev-a")
	}
	if got := metadata["queueName"]; got != "releasea.worker" {
		t.Fatalf("queueName = %v, want %q", got, "releasea.worker")
	}
	if got := metadata["leaseTTLSeconds"]; got != 180 {
		t.Fatalf("leaseTTLSeconds = %v, want %d", got, 180)
	}
	if got := metadata["claimedAt"]; got != "2026-03-29T20:00:00Z" {
		t.Fatalf("claimedAt = %v, want %q", got, "2026-03-29T20:00:00Z")
	}
	if got := metadata["leaseExpiresAt"]; got != "2026-03-29T20:03:00Z" {
		t.Fatalf("leaseExpiresAt = %v, want %q", got, "2026-03-29T20:03:00Z")
	}
}
