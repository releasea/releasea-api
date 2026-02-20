package operations

import (
	"testing"

	"releaseaapi/api/v1/shared"
)

func TestBuildGateways_InternalOnly(t *testing.T) {
	gateways := BuildGateways(nil, true, false, "prod")
	if len(gateways) < 1 {
		t.Error("internal gateway should include the internal gateway ref")
	}
}

func TestBuildGateways_ExternalOnly(t *testing.T) {
	gateways := BuildGateways(nil, false, true, "prod")
	if len(gateways) == 0 {
		t.Error("external gateway should include at least one gateway")
	}
}

func TestBuildGateways_BothInternalAndExternal(t *testing.T) {
	gateways := BuildGateways(nil, true, true, "prod")
	if len(gateways) < 2 {
		t.Errorf("both gateways should produce at least 2 entries, got %d: %v", len(gateways), gateways)
	}
}

func TestBuildGateways_NeitherClearsAll(t *testing.T) {
	existing := []string{"istio-system/releasea-internal-gateway", "istio-system/releasea-external-gateway"}
	gateways := BuildGateways(existing, false, false, "prod")
	if len(gateways) != 0 {
		t.Errorf("neither internal nor external should clear all gateways, got %v", gateways)
	}
}

func TestBuildGateways_PreservesExternalGateways(t *testing.T) {
	existing := []string{"custom-ns/my-gateway"}
	gateways := BuildGateways(existing, false, true, "prod")
	found := false
	for _, gw := range gateways {
		if gw == "custom-ns/my-gateway" {
			found = true
		}
	}
	if !found {
		t.Errorf("should preserve existing external gateway, got %v", gateways)
	}
}

func TestUniqueStrings(t *testing.T) {
	input := []string{"a", "b", "a", "c", "b"}
	got := shared.UniqueStrings(input)
	if len(got) != 3 {
		t.Errorf("expected 3 unique strings, got %d: %v", len(got), got)
	}
}

func TestToStringSlice_Nil(t *testing.T) {
	got := shared.ToStringSlice(nil)
	if len(got) != 0 {
		t.Errorf("nil should return empty slice, got %v", got)
	}
}

func TestToStringSlice_StringSlice(t *testing.T) {
	input := []string{"a", "b"}
	got := shared.ToStringSlice(input)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("expected [a, b], got %v", got)
	}
}

func TestToStringSlice_InterfaceSlice(t *testing.T) {
	input := []interface{}{"a", "b", 123}
	got := shared.ToStringSlice(input)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("expected [a, b], got %v", got)
	}
}

func TestMapPayload_NilReturnsEmptyMap(t *testing.T) {
	got := shared.MapPayload(nil)
	if len(got) != 0 {
		t.Errorf("nil should return empty map, got %v", got)
	}
}

func TestBoolValue(t *testing.T) {
	cases := []struct {
		input interface{}
		want  bool
	}{
		{true, true},
		{false, false},
		{"true", true},
		{"1", true},
		{"false", false},
		{nil, false},
		{42, false},
	}
	for _, tc := range cases {
		got := shared.BoolValue(tc.input)
		if got != tc.want {
			t.Errorf("shared.BoolValue(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestIntValue(t *testing.T) {
	cases := []struct {
		input interface{}
		want  int
	}{
		{42, 42},
		{int32(100), 100},
		{int64(200), 200},
		{float64(3.14), 3},
		{float32(2.7), 2},
		{nil, 0},
		{"not a number", 0},
	}
	for _, tc := range cases {
		got := shared.IntValue(tc.input)
		if got != tc.want {
			t.Errorf("shared.IntValue(%v) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestBuildGateways_InternalForStaging(t *testing.T) {
	gateways := BuildGateways(nil, true, false, "staging")
	if len(gateways) < 1 {
		t.Errorf("internal should produce at least 1 gateway, got %d: %v", len(gateways), gateways)
	}
}
