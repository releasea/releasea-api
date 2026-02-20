package services

import (
	"log"
	"releaseaapi/config"
)

// Setup seeds essential data when RELEASEA_SETUP=true.
func Setup(cfg *config.Config) {
	didReset := false
	if cfg == nil || !cfg.DoSetup {
		log.Println("[setup] RELEASEA_SETUP=false, skipping seed.")
	} else {
		log.Println("[setup] RELEASEA_SETUP=true, resetting database and seeding defaults...")
		if err := seedDefaults(true); err != nil {
			log.Fatalf("[setup] failed to reset database: %v", err)
		}
		didReset = true
	}

	if err := ensureBootstrapIdentity(cfg); err != nil {
		if didReset {
			log.Fatalf("[setup] bootstrap identity failed after reset: %v", err)
		}
		log.Fatalf("[setup] bootstrap identity failed: %v", err)
	}

	if err := ensurePlatformDefaults(cfg); err != nil {
		if didReset {
			log.Fatalf("[setup] platform defaults failed after reset: %v", err)
		}
		log.Fatalf("[setup] platform defaults failed: %v", err)
	}

	if cfg == nil || !cfg.InstallTemplates {
		log.Println("[setup] INSTALL_TEMPLATES=false, skipping template installation.")
		return
	}

	if err := InstallTemplates(cfg); err != nil {
		if didReset {
			log.Fatalf("[setup] template installation failed after reset: %v", err)
		}
		log.Printf("[setup] template installation failed: %v", err)
	}
}
