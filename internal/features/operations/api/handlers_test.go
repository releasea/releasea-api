package operations

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestApplyOperationFairnessByResourceInterleavesResources(t *testing.T) {
	items := []bson.M{
		{"id": "op-1", "resourceType": "service", "resourceId": "svc-a"},
		{"id": "op-2", "resourceType": "service", "resourceId": "svc-a"},
		{"id": "op-3", "resourceType": "service", "resourceId": "svc-b"},
		{"id": "op-4", "resourceType": "service", "resourceId": "svc-b"},
		{"id": "op-5", "resourceType": "service", "resourceId": "svc-c"},
	}

	got := applyOperationFairnessByResource(items)
	ids := []string{
		got[0]["id"].(string),
		got[1]["id"].(string),
		got[2]["id"].(string),
		got[3]["id"].(string),
		got[4]["id"].(string),
	}
	want := []string{"op-1", "op-3", "op-5", "op-2", "op-4"}
	for index := range want {
		if ids[index] != want[index] {
			t.Fatalf("order[%d] = %q, want %q (full=%v)", index, ids[index], want[index], ids)
		}
	}
}

func TestOperationFairnessKeyFallsBackToOperationID(t *testing.T) {
	got := operationFairnessKey(bson.M{"id": "op-1"})
	if got != "operation:op-1" {
		t.Fatalf("operationFairnessKey = %q, want %q", got, "operation:op-1")
	}
}
