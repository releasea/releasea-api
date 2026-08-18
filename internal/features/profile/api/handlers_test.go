package profile

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestAllowedProfileUpdatesRejectsIdentityAndAuthorizationFields(t *testing.T) {
	updates := allowedProfileUpdates(bson.M{
		"id":                 "user-other",
		"role":               "admin",
		"teamId":             "team-other",
		"sessions":           bson.A{"session-other"},
		"connectedProviders": bson.A{"provider-other"},
		"name":               "  Maia  ",
		"email":              "  MAIA@EXAMPLE.COM ",
		"twoFactorEnabled":   true,
	})

	if len(updates) != 3 {
		t.Fatalf("expected three safe fields, got %#v", updates)
	}
	if updates["name"] != "Maia" || updates["email"] != "maia@example.com" || updates["twoFactorEnabled"] != true {
		t.Fatalf("unexpected normalized profile update: %#v", updates)
	}
	for _, forbidden := range []string{"id", "role", "teamId", "sessions", "connectedProviders"} {
		if _, ok := updates[forbidden]; ok {
			t.Fatalf("forbidden field %q was accepted", forbidden)
		}
	}
}
