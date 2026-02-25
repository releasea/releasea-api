package shared

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestToStringSlice(t *testing.T) {
	if got := ToStringSlice(nil); len(got) != 0 {
		t.Fatalf("ToStringSlice(nil) should return empty slice")
	}

	input := []string{"a", "b"}
	got := ToStringSlice(input)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ToStringSlice([]string) unexpected result: %#v", got)
	}

	// Ensure copy for []string input.
	got[0] = "mutated"
	if input[0] != "a" {
		t.Fatalf("ToStringSlice should not share backing array for []string input")
	}

	mixed := []interface{}{"a", 10, "b"}
	got = ToStringSlice(mixed)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("ToStringSlice([]interface{}) unexpected result: %#v", got)
	}
}

func TestToInterfaceSlice(t *testing.T) {
	input := []interface{}{"a", 1}
	got := ToInterfaceSlice(input)
	if len(got) != 2 {
		t.Fatalf("ToInterfaceSlice should keep interface slice")
	}

	if got := ToInterfaceSlice("invalid"); got != nil {
		t.Fatalf("ToInterfaceSlice(non-slice) should return nil")
	}
}

func TestMapPayload(t *testing.T) {
	m := bson.M{"id": "abc"}
	got := MapPayload(m)
	if got["id"] != "abc" {
		t.Fatalf("MapPayload(bson.M) failed: %#v", got)
	}

	got = MapPayload(map[string]interface{}{"id": "xyz"})
	if got["id"] != "xyz" {
		t.Fatalf("MapPayload(map[string]interface{}) failed: %#v", got)
	}

	got = MapPayload(nil)
	if len(got) != 0 {
		t.Fatalf("MapPayload(nil) should return empty map")
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("TEST_ENV_OR_DEFAULT", "")
	if got := EnvOrDefault("TEST_ENV_OR_DEFAULT", "fallback"); got != "fallback" {
		t.Fatalf("EnvOrDefault should return fallback, got %q", got)
	}

	t.Setenv("TEST_ENV_OR_DEFAULT", "value")
	if got := EnvOrDefault("TEST_ENV_OR_DEFAULT", "fallback"); got != "value" {
		t.Fatalf("EnvOrDefault should return env value, got %q", got)
	}

	t.Setenv("TEST_ENV_BOOL", "true")
	if !EnvBool("TEST_ENV_BOOL", false) {
		t.Fatalf("EnvBool(true) should be true")
	}

	t.Setenv("TEST_ENV_BOOL", "invalid")
	if !EnvBool("TEST_ENV_BOOL", true) {
		t.Fatalf("EnvBool(invalid) should fallback to true")
	}
}

func TestNameHelpers(t *testing.T) {
	if got := ToKubeName("My_Service Name!"); got != "my-service-name" {
		t.Fatalf("ToKubeName unexpected result: %q", got)
	}

	if got := CanonicalRuleName("my-service-default", "my-service"); got != DefaultRuleName {
		t.Fatalf("CanonicalRuleName should map legacy default to canonical default")
	}

	if got := CanonicalRuleName("custom", "my-service"); got != "custom" {
		t.Fatalf("CanonicalRuleName should preserve custom name")
	}
}
