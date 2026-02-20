package observability

import (
	"testing"
	"time"

	"releaseaapi/api/v1/shared"
)

func TestParseMetricsRange_DefaultsToLastHour(t *testing.T) {
	start, end, step := ParseMetricsRange("", "")
	if end.Before(start) {
		t.Error("end should be after start")
	}
	diff := end.Sub(start)
	if diff < 59*time.Minute || diff > 61*time.Minute {
		t.Errorf("expected ~1 hour range, got %v", diff)
	}
	if step <= 0 {
		t.Error("step must be positive")
	}
}

func TestParseMetricsRange_ParsesRFC3339(t *testing.T) {
	from := "2024-06-01T10:00:00Z"
	to := "2024-06-01T11:00:00Z"
	start, end, step := ParseMetricsRange(from, to)
	if start.Format(time.RFC3339) != from {
		t.Errorf("start = %s, want %s", start.Format(time.RFC3339), from)
	}
	if end.Format(time.RFC3339) != to {
		t.Errorf("end = %s, want %s", end.Format(time.RFC3339), to)
	}
	if step <= 0 {
		t.Error("step must be positive")
	}
}

func TestParseMetricsRange_SwapsIfInverted(t *testing.T) {
	from := "2024-06-01T12:00:00Z"
	to := "2024-06-01T10:00:00Z"
	start, end, _ := ParseMetricsRange(from, to)
	if end.Before(start) {
		t.Error("should auto-swap inverted range")
	}
}

func TestBuildTimestamps_ReturnsAtLeastOneEntry(t *testing.T) {
	now := time.Now().UTC()
	ts := BuildTimestamps(now, now, time.Minute)
	if len(ts) == 0 {
		t.Error("expected at least one timestamp")
	}
}

func TestBuildTimestamps_StepAlignment(t *testing.T) {
	start := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2024, 6, 1, 11, 0, 0, 0, time.UTC)
	step := 15 * time.Minute
	ts := BuildTimestamps(start, end, step)
	// 10:00, 10:15, 10:30, 10:45, 11:00 = 5 entries
	if len(ts) != 5 {
		t.Errorf("expected 5 timestamps, got %d", len(ts))
	}
}

func TestFillSeries_MapsSamplesToCorrectIndex(t *testing.T) {
	start := time.Date(2024, 6, 1, 10, 0, 0, 0, time.UTC)
	step := time.Minute
	samples := []PromSample{
		{Time: start, Value: 42.0},
		{Time: start.Add(2 * time.Minute), Value: 99.0},
	}
	out := FillSeries(samples, start, step, 5)
	if out[0] != 42.0 {
		t.Errorf("out[0] = %f, want 42.0", out[0])
	}
	if out[1] != 0.0 {
		t.Errorf("out[1] = %f, want 0.0 (no sample)", out[1])
	}
	if out[2] != 99.0 {
		t.Errorf("out[2] = %f, want 99.0", out[2])
	}
}

func TestFillSeries_EmptySamples(t *testing.T) {
	start := time.Now().UTC()
	out := FillSeries(nil, start, time.Minute, 10)
	if len(out) != 10 {
		t.Errorf("expected 10 entries, got %d", len(out))
	}
	for i, v := range out {
		if v != 0 {
			t.Errorf("out[%d] = %f, want 0", i, v)
		}
	}
}

func TestDetectLogLevel(t *testing.T) {
	cases := []struct {
		message string
		want    string
	}{
		{"Something went wrong: ERROR", "error"},
		{"WARN: disk space low", "warn"},
		{"DEBUG: processing request", "debug"},
		{"INFO: server started", "info"},
		{"PANIC: unrecoverable", "error"},
		{"just a normal message", "info"},
	}
	for _, tc := range cases {
		got := detectLogLevel(tc.message)
		if got != tc.want {
			t.Errorf("detectLogLevel(%q) = %q, want %q", tc.message, got, tc.want)
		}
	}
}

func TestObservabilityNamespace_MatchesNamespaceResolver(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"prod", shared.NamespaceProduction},
		{"staging", shared.NamespaceStaging},
		{"dev", shared.NamespaceDevelopment},
	}
	for _, tc := range cases {
		got := NamespaceForEnvironment(tc.env)
		if got != tc.want {
			t.Errorf("NamespaceForEnvironment(%q) = %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestKubeName_SanitizesNames(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"My Service", "my-service"},
		{"api-gateway", "api-gateway"},
		{"Service_Name!@#", "service-name"},
		{"  spaces  ", "spaces"},
	}
	for _, tc := range cases {
		got := KubeName(tc.input)
		if got != tc.want {
			t.Errorf("KubeName(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
