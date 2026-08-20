package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEnvFilesUsesLastProfileAsOverride(t *testing.T) {
	tempDir := t.TempDir()
	composeFile := filepath.Join(tempDir, ".env.local.compose")
	clusterFile := filepath.Join(tempDir, ".env.local.cluster")

	if err := os.WriteFile(composeFile, []byte("LOKI_URL=\nPROMETHEUS_URL=http://compose:9090\n"), 0o600); err != nil {
		t.Fatalf("write compose profile: %v", err)
	}
	if err := os.WriteFile(clusterFile, []byte("LOKI_URL=http://localhost:3100\nPROMETHEUS_URL=http://localhost:9090\n"), 0o600); err != nil {
		t.Fatalf("write cluster profile: %v", err)
	}

	values := readEnvFiles([]string{composeFile, clusterFile})
	if got := values["LOKI_URL"]; got != "http://localhost:3100" {
		t.Fatalf("LOKI_URL = %q, want cluster profile URL", got)
	}
	if got := values["PROMETHEUS_URL"]; got != "http://localhost:9090" {
		t.Fatalf("PROMETHEUS_URL = %q, want cluster profile URL", got)
	}
}
