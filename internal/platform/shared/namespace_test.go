package shared

import (
	"strings"
	"testing"
)

func TestLoadNamespaceMappingUsesDefaults(t *testing.T) {
	t.Setenv("RELEASEA_NAMESPACE_MAPPING", "")

	mapping := LoadNamespaceMapping()
	if mapping["prod"] != NamespaceProduction {
		t.Fatalf("prod namespace = %q, want %q", mapping["prod"], NamespaceProduction)
	}
	if mapping["staging"] != NamespaceStaging {
		t.Fatalf("staging namespace = %q, want %q", mapping["staging"], NamespaceStaging)
	}
	if mapping["dev"] != NamespaceDevelopment {
		t.Fatalf("dev namespace = %q, want %q", mapping["dev"], NamespaceDevelopment)
	}
}

func TestLoadNamespaceMappingAppliesOnlyValidOverrides(t *testing.T) {
	t.Setenv("RELEASEA_NAMESPACE_MAPPING", `{
		"PROD":"releasea-apps-staging",
		"preview":"releasea-apps-production",
		"bad":"releasea-system",
		"empty":" "
	}`)

	mapping := LoadNamespaceMapping()

	if mapping["prod"] != NamespaceStaging {
		t.Fatalf("prod override = %q, want %q", mapping["prod"], NamespaceStaging)
	}
	if mapping["preview"] != NamespaceProduction {
		t.Fatalf("preview override = %q, want %q", mapping["preview"], NamespaceProduction)
	}
	if _, ok := mapping["bad"]; ok {
		t.Fatalf("invalid namespace override should be ignored")
	}
	if _, ok := mapping["empty"]; ok {
		t.Fatalf("empty namespace override should be ignored")
	}
}

func TestLoadNamespaceMappingIgnoresInvalidJSON(t *testing.T) {
	t.Setenv("RELEASEA_NAMESPACE_MAPPING", `{invalid-json`)

	mapping := LoadNamespaceMapping()
	if mapping["prod"] != NamespaceProduction {
		t.Fatalf("invalid JSON should keep defaults")
	}
}

func TestResolveAppNamespace(t *testing.T) {
	t.Setenv("RELEASEA_NAMESPACE_MAPPING", `{"my-custom":"releasea-apps-production"}`)

	if got := ResolveAppNamespace(""); got != NamespaceProduction {
		t.Fatalf("empty environment = %q, want %q", got, NamespaceProduction)
	}
	if got := ResolveAppNamespace("my-custom"); got != NamespaceProduction {
		t.Fatalf("custom environment = %q, want %q", got, NamespaceProduction)
	}
	if got := ResolveAppNamespace("unknown-env"); got != NamespaceDevelopment {
		t.Fatalf("unknown environment = %q, want %q", got, NamespaceDevelopment)
	}
}

func TestNamespaceValidationHelpers(t *testing.T) {
	if !IsValidAppNamespace(NamespaceProduction) || !IsValidAppNamespace(NamespaceStaging) || !IsValidAppNamespace(NamespaceDevelopment) {
		t.Fatalf("expected all app namespaces to be valid")
	}
	if IsValidAppNamespace(NamespaceSystem) {
		t.Fatalf("system namespace must not be valid app namespace")
	}

	if !IsSystemNamespace("  RELEASEA-SYSTEM ") {
		t.Fatalf("system namespace detection should be case-insensitive and trimmed")
	}

	if err := ValidateAppNamespace(NamespaceSystem); err == nil || !strings.Contains(err.Error(), "reserved") {
		t.Fatalf("expected reserved namespace validation error, got %v", err)
	}
	if err := ValidateAppNamespace("invalid-namespace"); err == nil || !strings.Contains(err.Error(), "not a valid application namespace") {
		t.Fatalf("expected invalid namespace error, got %v", err)
	}
	if err := ValidateAppNamespace(NamespaceStaging); err != nil {
		t.Fatalf("expected staging namespace to be valid, got %v", err)
	}
}

func TestEnvironmentsShareNamespace(t *testing.T) {
	t.Setenv("RELEASEA_NAMESPACE_MAPPING", `{"custom":"releasea-apps-production"}`)

	if !EnvironmentsShareNamespace("prod", "production") {
		t.Fatalf("prod and production should share namespace")
	}
	if EnvironmentsShareNamespace("prod", "dev") {
		t.Fatalf("prod and dev should not share namespace")
	}
	if !EnvironmentsShareNamespace("custom", "prod") {
		t.Fatalf("custom override and prod should share namespace")
	}
}
