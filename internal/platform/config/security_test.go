package config

import "testing"

func TestValidateProductionSecurityRejectsDefaults(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "change-me")
	t.Setenv("WORKER_JWT_SECRET", "worker-secret-that-is-at-least-32-chars")
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err := ValidateProductionSecurity(&Config{DefaultAdminPass: "releasea"}); err == nil {
		t.Fatal("expected insecure production configuration to be rejected")
	}
}

func TestValidateProductionSecurityAcceptsConfiguredSecrets(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("JWT_SECRET", "jwt-secret-that-is-at-least-32-characters")
	t.Setenv("WORKER_JWT_SECRET", "worker-secret-that-is-at-least-32-chars")
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=")
	if err := ValidateProductionSecurity(&Config{DefaultAdminPass: "admin-secret"}); err != nil {
		t.Fatalf("expected secure production configuration, got %v", err)
	}
}
