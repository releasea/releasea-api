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

func TestNormalizeRulePolicyAcceptsEdgeControls(t *testing.T) {
	policy, err := normalizeRulePolicy(map[string]interface{}{
		"action":          "allow",
		"authMode":        "apikey",
		"apiKeyHeader":    "X-API-Key",
		"apiKeys":         []interface{}{"gateway-key"},
		"ipPolicy":        "allowlist",
		"ipList":          []interface{}{"203.0.113.8"},
		"requiredHeaders": []interface{}{"X-Request-Id"},
		"timeoutMs":       2500,
		"retries":         1,
		"securityHeaders": true,
		"cors": map[string]interface{}{
			"enabled": true,
			"origins": []interface{}{"https://app.example.com"},
		},
		"rateLimit": map[string]interface{}{
			"requests":      100,
			"windowSeconds": 60,
		},
		"circuitBreaker": map[string]interface{}{
			"consecutiveErrors": 5,
		},
		"maxBodyBytes": 2048,
		"lockNetwork":  true,
	})
	if err != nil {
		t.Fatalf("expected valid edge policy: %v", err)
	}
	if got := shared.StringValue(policy["authMode"]); got != "apikey" {
		t.Fatalf("expected apikey mode, got %q", got)
	}
	if got := shared.ToStringSlice(policy["ipList"]); len(got) != 1 || got[0] != "203.0.113.8/32" {
		t.Fatalf("expected normalized CIDR, got %#v", got)
	}
	if got := shared.IntValue(policy["timeoutMs"]); got != 2500 {
		t.Fatalf("expected timeout 2500, got %d", got)
	}
	if !shared.BoolValue(policy["lockNetwork"]) {
		t.Fatal("expected network lock")
	}
}

func TestNormalizeRulePolicyRejectsInvalidEdgeControls(t *testing.T) {
	if _, err := normalizeRulePolicy(map[string]interface{}{
		"action":   "allow",
		"authMode": "apikey",
	}); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Fatalf("expected API key error, got %v", err)
	}
	if _, err := normalizeRulePolicy(map[string]interface{}{
		"action":   "allow",
		"ipPolicy": "allowlist",
		"ipList":   []interface{}{"not-an-ip"},
	}); err == nil || !strings.Contains(err.Error(), "invalid IP") {
		t.Fatalf("expected IP error, got %v", err)
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
