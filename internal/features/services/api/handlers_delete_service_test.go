package services

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestDeployBlocksServiceDeletion(t *testing.T) {
	tests := []struct {
		name   string
		deploy bson.M
		expect bool
	}{
		{
			name: "requested deploy blocks deletion",
			deploy: bson.M{
				"status": "requested",
			},
			expect: true,
		},
		{
			name: "deploying deploy blocks deletion",
			deploy: bson.M{
				"status": "deploying",
			},
			expect: true,
		},
		{
			name: "rollback without finishedAt blocks deletion",
			deploy: bson.M{
				"status": "rollback",
			},
			expect: true,
		},
		{
			name: "rollback with empty finishedAt blocks deletion",
			deploy: bson.M{
				"status":     "rollback",
				"finishedAt": "",
			},
			expect: true,
		},
		{
			name: "rollback with finishedAt does not block deletion",
			deploy: bson.M{
				"status":     "rollback",
				"finishedAt": "2026-02-26T00:00:00Z",
			},
			expect: false,
		},
		{
			name: "unknown status blocks deletion defensively",
			deploy: bson.M{
				"status": "custom",
			},
			expect: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deployBlocksServiceDeletion(tt.deploy)
			if got != tt.expect {
				t.Fatalf("deployBlocksServiceDeletion(%v) = %v, want %v", tt.deploy, got, tt.expect)
			}
		})
	}
}
