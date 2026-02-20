// config/config.go
package config

import (
	"os"
	"strings"
)

type Config struct {
	Port                string
	MongoURI            string
	DoSetup             bool
	InstallTemplates    bool
	TemplatesRepoOwner  string
	TemplatesRepoName   string
	TemplatesRepoRef    string
	DefaultTeamID       string
	DefaultTeamName     string
	DefaultAdminID      string
	DefaultAdminName    string
	DefaultAdminEmail   string
	DefaultAdminPass    string
	KeepAdditionalUsers bool
	AllowUserSignup     bool
}

func LoadConfig() *Config {
	return &Config{
		Port:                env("PORT", "8070"),
		MongoURI:            os.Getenv("MONGO_URI"),
		DoSetup:             envBool("RELEASEA_SETUP", false),
		InstallTemplates:    envBool("INSTALL_TEMPLATES", true),
		TemplatesRepoOwner:  env("TEMPLATE_REPO_OWNER", "releasea"),
		TemplatesRepoName:   env("TEMPLATE_REPO_NAME", "templates"),
		TemplatesRepoRef:    env("TEMPLATE_REPO_REF", "main"),
		DefaultTeamID:       env("DEFAULT_TEAM_ID", "team-1"),
		DefaultTeamName:     env("DEFAULT_TEAM_NAME", "Platform"),
		DefaultAdminID:      env("DEFAULT_ADMIN_ID", "user-1"),
		DefaultAdminName:    env("DEFAULT_ADMIN_NAME", "Platform Admin"),
		DefaultAdminEmail:   strings.ToLower(env("DEFAULT_ADMIN_EMAIL", "admin@releasea.io")),
		DefaultAdminPass:    env("DEFAULT_ADMIN_PASSWORD", "releasea"),
		KeepAdditionalUsers: envBool("KEEP_ADDITIONAL_USERS", false),
		AllowUserSignup:     envBool("ALLOW_USER_SIGNUP", false),
	}
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func envBool(k string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(k)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "y":
		return true
	case "0", "false", "no", "n":
		return false
	default:
		return fallback
	}
}
