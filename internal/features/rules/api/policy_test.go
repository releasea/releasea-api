package rules

import (
	"strings"
	"testing"

	"releaseaapi/internal/platform/shared"
)

func TestNormalizeRulePolicyDefaultsToPublicAccess(t *testing.T) {
	policy, err := normalizeRulePolicy(map[string]interface{}{"action": "allow"})
	if err != nil {
		t.Fatalf("expected valid public policy: %v", err)
	}
	if got := shared.StringValue(policy["authMode"]); got != "public" {
		t.Fatalf("expected public access, got %q", got)
	}
}

func TestNormalizeRulePolicyRequiresJWTProviderMetadata(t *testing.T) {
	_, err := normalizeRulePolicy(map[string]interface{}{"action": "allow", "authMode": "jwt"})
	if err == nil || !strings.Contains(err.Error(), "JWT issuer") {
		t.Fatalf("expected missing issuer error, got %v", err)
	}
}

func TestNormalizeRulePolicyAcceptsOIDCJWTConfiguration(t *testing.T) {
	policy, err := normalizeRulePolicy(map[string]interface{}{
		"action":    "allow",
		"authMode":  "jwt",
		"jwtIssuer": "https://id.example.com/realms/platform",
		"jwksUri":   "https://id.example.com/realms/platform/protocol/openid-connect/certs",
		"audiences": []interface{}{"payments-api", "internal-clients"},
	})
	if err != nil {
		t.Fatalf("expected valid JWT policy: %v", err)
	}
	if got := shared.StringValue(policy["authMode"]); got != "jwt" {
		t.Fatalf("expected jwt access, got %q", got)
	}
	if got := shared.ToStringSlice(policy["audiences"]); len(got) != 2 {
		t.Fatalf("expected two audiences, got %#v", got)
	}
}

func TestNormalizeRulePolicyForcesDeniedRoutesToPublicAuthMode(t *testing.T) {
	policy, err := normalizeRulePolicy(map[string]interface{}{
		"action":    "deny",
		"authMode":  "jwt",
		"jwtIssuer": "invalid",
		"jwksUri":   "invalid",
	})
	if err != nil {
		t.Fatalf("deny routes should not require JWT metadata: %v", err)
	}
	if got := shared.StringValue(policy["authMode"]); got != "public" {
		t.Fatalf("expected deny policy to ignore auth mode, got %q", got)
	}
}
