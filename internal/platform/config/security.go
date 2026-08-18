package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
)

var productionMarkers = []string{"production", "prod", "staging", "stage"}

func IsProductionLike() bool {
	for _, key := range []string{"RELEASEA_RUNTIME_ENV", "RELEASEA_ENV", "APP_ENV", "ENVIRONMENT"} {
		value := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		for _, marker := range productionMarkers {
			if value == marker {
				return true
			}
		}
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GIN_MODE")), "release")
}

func ValidateProductionSecurity(cfg *Config) error {
	if !IsProductionLike() {
		return nil
	}
	required := map[string]string{
		"JWT_SECRET":                os.Getenv("JWT_SECRET"),
		"WORKER_JWT_SECRET":         os.Getenv("WORKER_JWT_SECRET"),
		"CREDENTIAL_ENCRYPTION_KEY": os.Getenv("CREDENTIAL_ENCRYPTION_KEY"),
	}
	for name, value := range required {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" || strings.EqualFold(trimmed, "change-me") {
			return fmt.Errorf("%s must be configured securely in production", name)
		}
		if name != "CREDENTIAL_ENCRYPTION_KEY" && len(trimmed) < 32 {
			return fmt.Errorf("%s must contain at least 32 characters in production", name)
		}
	}
	decodedKey, err := base64.StdEncoding.DecodeString(strings.TrimSpace(required["CREDENTIAL_ENCRYPTION_KEY"]))
	if err != nil || len(decodedKey) != 32 {
		return fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key in production")
	}
	if cfg == nil || len(strings.TrimSpace(cfg.DefaultAdminPass)) < 12 || strings.EqualFold(strings.TrimSpace(cfg.DefaultAdminPass), "releasea") {
		return fmt.Errorf("DEFAULT_ADMIN_PASSWORD must be configured securely in production")
	}
	return nil
}
