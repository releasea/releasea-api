package operations

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestBuildStaleClaimRecoveryMutationRequeuesUntilLimit(t *testing.T) {
	now := time.Date(2026, time.March, 29, 21, 0, 0, 0, time.UTC)
	mutation := buildStaleClaimRecoveryMutation(bson.M{
		"claim": bson.M{
			"leaseExpiresAt":       "2026-03-29T20:58:00Z",
			"holder":               "worker-a",
			"workerRegistrationId": "reg-a",
		},
		"recovery": bson.M{
			"count": 1,
		},
	}, "worker-b", now)

	if mutation.Status != StatusQueued {
		t.Fatalf("status = %q, want %q", mutation.Status, StatusQueued)
	}
	if mutation.AuditAction != "operation.claim.recovered" {
		t.Fatalf("audit action = %q", mutation.AuditAction)
	}
	if got := mutation.Set["recovery.count"]; got != 2 {
		t.Fatalf("recovery count = %v, want %d", got, 2)
	}
	if got := mutation.Set["recovery.lastDisposition"]; got != "requeued" {
		t.Fatalf("last disposition = %v, want %q", got, "requeued")
	}
	if got := mutation.Unset["startedAt"]; got != "" {
		t.Fatalf("startedAt unset = %v, want empty marker", got)
	}
}

func TestBuildStaleClaimRecoveryMutationFailsAfterLimit(t *testing.T) {
	now := time.Date(2026, time.March, 29, 21, 0, 0, 0, time.UTC)
	mutation := buildStaleClaimRecoveryMutation(bson.M{
		"claim": bson.M{
			"leaseExpiresAt":       "2026-03-29T20:58:00Z",
			"holder":               "worker-a",
			"workerRegistrationId": "reg-a",
		},
		"recovery": bson.M{
			"count": defaultStaleClaimRecoveryLimit - 1,
		},
	}, "worker-b", now)

	if mutation.Status != StatusFailed {
		t.Fatalf("status = %q, want %q", mutation.Status, StatusFailed)
	}
	if mutation.AuditAction != "operation.claim.exhausted" {
		t.Fatalf("audit action = %q", mutation.AuditAction)
	}
	if got := mutation.Set["recovery.lastDisposition"]; got != "failed" {
		t.Fatalf("last disposition = %v, want %q", got, "failed")
	}
	if got := mutation.Set["finishedAt"]; got != "2026-03-29T21:00:00Z" {
		t.Fatalf("finishedAt = %v, want %q", got, "2026-03-29T21:00:00Z")
	}
	if _, ok := mutation.Unset["finishedAt"]; ok {
		t.Fatalf("finishedAt should not be unset for failed recovery")
	}
}
