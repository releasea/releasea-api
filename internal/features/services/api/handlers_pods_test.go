package services

import (
	"reflect"
	"testing"

	observability "releaseaapi/internal/features/observability/api"
)

func TestPodNamesByLatestLogOrdersMostRecentInstanceFirst(t *testing.T) {
	logs := []observability.LogPayload{
		{Timestamp: "2026-08-20T10:00:00Z", Metadata: map[string]interface{}{"replicaName": "service-old"}},
		{Timestamp: "2026-08-20T11:00:00Z", Metadata: map[string]interface{}{"replicaName": "service-current"}},
		{Timestamp: "2026-08-20T11:01:00Z", Metadata: map[string]interface{}{"replicaName": "service-current"}},
		{Timestamp: "2026-08-20T10:30:00Z", Metadata: map[string]interface{}{}},
	}

	want := []string{"service-current", "service-old"}
	if got := podNamesByLatestLog(logs); !reflect.DeepEqual(got, want) {
		t.Fatalf("pod order = %v, want %v", got, want)
	}
}
