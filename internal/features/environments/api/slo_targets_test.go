package environments

import "testing"

func TestNormalizeEnvironmentSLOTargetsDefaults(t *testing.T) {
	got, err := normalizeEnvironmentSLOTargets(nil)
	if err != nil {
		t.Fatalf("normalizeEnvironmentSLOTargets(nil) error = %v", err)
	}

	if got["availabilityPct"] != defaultEnvironmentAvailabilityTargetPct {
		t.Fatalf("availabilityPct = %v, want %v", got["availabilityPct"], defaultEnvironmentAvailabilityTargetPct)
	}
	if got["latencyP95Ms"] != defaultEnvironmentLatencyTargetMs {
		t.Fatalf("latencyP95Ms = %v, want %v", got["latencyP95Ms"], defaultEnvironmentLatencyTargetMs)
	}
}

func TestNormalizeEnvironmentSLOTargetsOverrides(t *testing.T) {
	availability := 99.9
	latency := 350
	got, err := normalizeEnvironmentSLOTargets(&sloTargetsPayload{
		AvailabilityPct: &availability,
		LatencyP95Ms:    &latency,
	})
	if err != nil {
		t.Fatalf("normalizeEnvironmentSLOTargets override error = %v", err)
	}

	if got["availabilityPct"] != availability {
		t.Fatalf("availabilityPct = %v, want %v", got["availabilityPct"], availability)
	}
	if got["latencyP95Ms"] != latency {
		t.Fatalf("latencyP95Ms = %v, want %v", got["latencyP95Ms"], latency)
	}
}

func TestNormalizeEnvironmentSLOTargetsRejectsInvalidAvailability(t *testing.T) {
	availability := 101.0
	if _, err := normalizeEnvironmentSLOTargets(&sloTargetsPayload{AvailabilityPct: &availability}); err == nil {
		t.Fatal("expected invalid availability target error")
	}
}

func TestNormalizeEnvironmentSLOTargetsRejectsInvalidLatency(t *testing.T) {
	latency := 0
	if _, err := normalizeEnvironmentSLOTargets(&sloTargetsPayload{LatencyP95Ms: &latency}); err == nil {
		t.Fatal("expected invalid latency target error")
	}
}
