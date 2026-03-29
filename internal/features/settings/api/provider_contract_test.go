package settings

import (
	"testing"

	platformmodels "releaseaapi/internal/platform/models"
)

func TestBuildProviderCatalogCompatibilityContracts(t *testing.T) {
	catalog := buildProviderCatalog()

	families := []struct {
		name            string
		category        platformmodels.ProviderCategory
		requireDefault  bool
		requireAuthMode bool
	}{
		{name: "scm", category: catalog.SCM, requireDefault: true, requireAuthMode: true},
		{name: "registry", category: catalog.Registry, requireDefault: true, requireAuthMode: true},
		{name: "secrets", category: catalog.Secrets, requireDefault: true, requireAuthMode: false},
		{name: "identity", category: catalog.Identity, requireDefault: false, requireAuthMode: false},
		{name: "notifications", category: catalog.Notifications, requireDefault: false, requireAuthMode: false},
	}

	for _, family := range families {
		if family.category.Kind == "" {
			t.Fatalf("%s kind missing", family.name)
		}
		if len(family.category.Providers) == 0 {
			t.Fatalf("%s providers missing", family.name)
		}

		defaultFound := family.category.DefaultProvider == ""
		for _, provider := range family.category.Providers {
			if provider.ID == "" {
				t.Fatalf("%s provider missing id", family.name)
			}
			if provider.Label == "" {
				t.Fatalf("%s provider %q missing label", family.name, provider.ID)
			}
			if family.requireAuthMode && len(provider.AuthModes) == 0 {
				t.Fatalf("%s provider %q missing auth modes", family.name, provider.ID)
			}
			if provider.ID == family.category.DefaultProvider {
				defaultFound = true
			}
		}

		if family.requireDefault && !defaultFound {
			t.Fatalf("%s default provider %q not found in catalog", family.name, family.category.DefaultProvider)
		}
	}
}
