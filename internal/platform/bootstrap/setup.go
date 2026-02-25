package bootstrap

import (
	"context"
	"log"

	"releaseaapi/internal/platform/config"
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

// Setup initializes base platform data.
// RELEASEA_RESET=true forces a destructive reset (drop + restore defaults).
func Setup(cfg *config.Config) {
	didReset := false
	if cfg == nil || !cfg.ResetOnStart {
		log.Println("[setup] RELEASEA_RESET=false, preserving existing database.")
	} else {
		log.Println("[setup] RELEASEA_RESET=true, resetting database and restoring base defaults...")
		if err := seedDefaults(true); err != nil {
			log.Fatalf("[setup] failed to reset database: %v", err)
		}
		didReset = true
	}

	firstBootstrap := didReset
	if !firstBootstrap {
		var err error
		firstBootstrap, err = requiresInitialBootstrap()
		if err != nil {
			log.Fatalf("[setup] failed to inspect bootstrap state: %v", err)
		}
		if firstBootstrap {
			log.Println("[setup] no base data found; bootstrapping platform defaults.")
		}
	}

	if err := ensureBootstrapIdentity(cfg, didReset || firstBootstrap); err != nil {
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

func requiresInitialBootstrap() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	count, err := shared.Collection(shared.PlatformSettingsCollection).CountDocuments(ctx, bson.M{})
	if err != nil {
		return false, err
	}
	return count == 0, nil
}
