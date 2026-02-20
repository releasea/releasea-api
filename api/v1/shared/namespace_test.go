package shared

import (
	"os"
	"testing"
)

func TestResolveAppNamespace_CoreEnvironments(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"prod", NamespaceProduction},
		{"production", NamespaceProduction},
		{"live", NamespaceProduction},
		{"staging", NamespaceStaging},
		{"stage", NamespaceStaging},
		{"uat", NamespaceStaging},
		{"pre-prod", NamespaceStaging},
		{"dev", NamespaceDevelopment},
		{"development", NamespaceDevelopment},
		{"qa", NamespaceDevelopment},
		{"sandbox", NamespaceDevelopment},
		{"test", NamespaceDevelopment},
		{"preview", NamespaceDevelopment},
		{"feature", NamespaceDevelopment},
		{"ci", NamespaceDevelopment},
	}
	for _, tc := range cases {
		t.Run(tc.env, func(t *testing.T) {
			got := ResolveAppNamespace(tc.env)
			if got != tc.want {
				t.Errorf("ResolveAppNamespace(%q) = %q, want %q", tc.env, got, tc.want)
			}
		})
	}
}

func TestResolveAppNamespace_EmptyDefaultsToProd(t *testing.T) {
	got := ResolveAppNamespace("")
	if got != NamespaceProduction {
		t.Errorf("ResolveAppNamespace(\"\") = %q, want %q", got, NamespaceProduction)
	}
}

func TestResolveAppNamespace_UnknownDefaultsToDev(t *testing.T) {
	cases := []string{"custom-env", "experiment", "demo", "perf"}
	for _, env := range cases {
		got := ResolveAppNamespace(env)
		if got != NamespaceDevelopment {
			t.Errorf("ResolveAppNamespace(%q) = %q, want %q", env, got, NamespaceDevelopment)
		}
	}
}

func TestResolveAppNamespace_CaseInsensitive(t *testing.T) {
	cases := []struct {
		env  string
		want string
	}{
		{"PROD", NamespaceProduction},
		{"Staging", NamespaceStaging},
		{"DEV", NamespaceDevelopment},
		{"  prod  ", NamespaceProduction},
	}
	for _, tc := range cases {
		got := ResolveAppNamespace(tc.env)
		if got != tc.want {
			t.Errorf("ResolveAppNamespace(%q) = %q, want %q", tc.env, got, tc.want)
		}
	}
}

func TestResolveAppNamespace_NeverReturnsSystemNamespace(t *testing.T) {
	envs := []string{"prod", "staging", "dev", "qa", "uat", "sandbox", "", "releasea-system", "system"}
	for _, env := range envs {
		got := ResolveAppNamespace(env)
		if got == NamespaceSystem {
			t.Errorf("ResolveAppNamespace(%q) returned releasea-system, which is forbidden", env)
		}
	}
}

func TestIsSystemNamespace(t *testing.T) {
	if !IsSystemNamespace("releasea-system") {
		t.Error("expected releasea-system to be detected as system namespace")
	}
	if !IsSystemNamespace("RELEASEA-SYSTEM") {
		t.Error("expected RELEASEA-SYSTEM to be detected as system namespace")
	}
	if IsSystemNamespace("releasea-apps-production") {
		t.Error("expected releasea-apps-production to NOT be detected as system namespace")
	}
}

func TestValidateAppNamespace(t *testing.T) {
	// Valid namespaces
	for _, ns := range []string{NamespaceProduction, NamespaceStaging, NamespaceDevelopment} {
		if err := ValidateAppNamespace(ns); err != nil {
			t.Errorf("ValidateAppNamespace(%q) returned error: %v", ns, err)
		}
	}

	// releasea-system must be rejected
	if err := ValidateAppNamespace(NamespaceSystem); err == nil {
		t.Error("ValidateAppNamespace(releasea-system) should return an error")
	}

	// random namespace must be rejected
	if err := ValidateAppNamespace("random-ns"); err == nil {
		t.Error("ValidateAppNamespace(random-ns) should return an error")
	}
}

func TestEnvironmentsShareNamespace(t *testing.T) {
	if !EnvironmentsShareNamespace("prod", "production") {
		t.Error("prod and production should share the same namespace")
	}
	if !EnvironmentsShareNamespace("dev", "qa") {
		t.Error("dev and qa should share the same namespace")
	}
	if EnvironmentsShareNamespace("prod", "staging") {
		t.Error("prod and staging should NOT share the same namespace")
	}
}

func TestResolveAppNamespace_CustomMapping(t *testing.T) {
	os.Setenv("RELEASEA_NAMESPACE_MAPPING", `{"perf":"releasea-apps-staging","demo":"releasea-apps-production"}`)
	defer os.Unsetenv("RELEASEA_NAMESPACE_MAPPING")

	if got := ResolveAppNamespace("perf"); got != NamespaceStaging {
		t.Errorf("ResolveAppNamespace(perf) = %q, want %q (from custom mapping)", got, NamespaceStaging)
	}
	if got := ResolveAppNamespace("demo"); got != NamespaceProduction {
		t.Errorf("ResolveAppNamespace(demo) = %q, want %q (from custom mapping)", got, NamespaceProduction)
	}
}

func TestResolveAppNamespace_InvalidCustomMappingIgnored(t *testing.T) {
	// Mapping to releasea-system should be silently ignored
	os.Setenv("RELEASEA_NAMESPACE_MAPPING", `{"evil":"releasea-system"}`)
	defer os.Unsetenv("RELEASEA_NAMESPACE_MAPPING")

	if got := ResolveAppNamespace("evil"); got == NamespaceSystem {
		t.Error("custom mapping to releasea-system should be rejected")
	}
}
