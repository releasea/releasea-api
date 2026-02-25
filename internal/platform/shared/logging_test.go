package shared

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
)

func TestLogInfoProducesStructuredJSON(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer log.SetOutput(prevWriter)
	defer log.SetFlags(prevFlags)

	LogInfo("service.deploy.requested", LogFields{
		"serviceId":   "svc-1",
		"environment": "prod",
	})

	entry := parseLogJSON(t, buf.String())
	if entry["level"] != "info" {
		t.Fatalf("level = %v, want info", entry["level"])
	}
	if entry["event"] != "service.deploy.requested" {
		t.Fatalf("event = %v, want service.deploy.requested", entry["event"])
	}
	if entry["serviceId"] != "svc-1" {
		t.Fatalf("serviceId = %v, want svc-1", entry["serviceId"])
	}
	if entry["environment"] != "prod" {
		t.Fatalf("environment = %v, want prod", entry["environment"])
	}
	if _, ok := entry["ts"]; !ok {
		t.Fatalf("expected ts field in structured log")
	}
}

func TestLogErrorAddsErrorField(t *testing.T) {
	var buf bytes.Buffer
	prevWriter := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer log.SetOutput(prevWriter)
	defer log.SetFlags(prevFlags)

	LogError("service.deploy.failed", errors.New("boom"), LogFields{
		"serviceId": "svc-2",
	})

	entry := parseLogJSON(t, buf.String())
	if entry["level"] != "error" {
		t.Fatalf("level = %v, want error", entry["level"])
	}
	if entry["error"] != "boom" {
		t.Fatalf("error = %v, want boom", entry["error"])
	}
}

func parseLogJSON(t *testing.T, raw string) map[string]interface{} {
	t.Helper()
	start := strings.Index(raw, "{")
	if start < 0 {
		t.Fatalf("log output does not contain JSON object: %q", raw)
	}

	var entry map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw[start:])), &entry); err != nil {
		t.Fatalf("failed to parse log JSON: %v (raw=%q)", err, raw)
	}
	return entry
}
