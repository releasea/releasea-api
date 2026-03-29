package services

import (
	"testing"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func TestSnapshotDigestIgnoresEmissionMetadata(t *testing.T) {
	left := serviceStatusSnapshot{
		Service:     bson.M{"id": "svc-1", "status": "running"},
		Deploys:     []bson.M{{"id": "dep-1", "status": "running"}},
		Rules:       []bson.M{},
		RuleDeploys: []bson.M{},
		Version:     "1",
		Cursor:      "cursor-a",
		EmittedAt:   "2026-03-29T10:00:00Z",
	}
	right := serviceStatusSnapshot{
		Service:     bson.M{"id": "svc-1", "status": "running"},
		Deploys:     []bson.M{{"id": "dep-1", "status": "running"}},
		Rules:       []bson.M{},
		RuleDeploys: []bson.M{},
		Version:     "2",
		Cursor:      "cursor-b",
		EmittedAt:   "2026-03-29T10:05:00Z",
	}

	leftDigest, err := snapshotDigest(left)
	if err != nil {
		t.Fatalf("left digest error: %v", err)
	}
	rightDigest, err := snapshotDigest(right)
	if err != nil {
		t.Fatalf("right digest error: %v", err)
	}
	if leftDigest != rightDigest {
		t.Fatalf("expected stable digest, got %q and %q", leftDigest, rightDigest)
	}
}

func TestFinalizeServicesStatusSnapshotSetsVersionAndCursor(t *testing.T) {
	snapshot, err := finalizeServicesStatusSnapshot(servicesStatusSnapshot{
		Services:  []bson.M{{"id": "svc-1"}},
		Deploys:   []bson.M{{"id": "dep-1"}},
		EmittedAt: "2026-03-29T10:00:00Z",
	})
	if err != nil {
		t.Fatalf("finalize snapshot: %v", err)
	}
	if snapshot.Version != statusSnapshotSchemaVersion {
		t.Fatalf("version = %q, want %q", snapshot.Version, statusSnapshotSchemaVersion)
	}
	if snapshot.Cursor == "" {
		t.Fatalf("expected cursor to be populated")
	}
}

func TestBuildLiveStateChangeEventClassifiesGitOpsAudit(t *testing.T) {
	event, ok := buildLiveStateChangeEvent("service:svc-1", serviceStatusChangeEvent{
		OperationType: "insert",
		Namespace: struct {
			Collection string `bson:"coll"`
		}{Collection: shared.PlatformAuditCollection},
		FullDocument: bson.M{
			"action":     "service.gitops_flux_pr.create",
			"resourceId": "svc-1",
		},
	})
	if !ok {
		t.Fatalf("expected change event to be built")
	}
	if event.Kind != "gitops" {
		t.Fatalf("kind = %q, want %q", event.Kind, "gitops")
	}
	if event.ResourceID != "svc-1" {
		t.Fatalf("resource id = %q, want %q", event.ResourceID, "svc-1")
	}
}
