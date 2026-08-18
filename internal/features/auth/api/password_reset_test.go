package auth

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

func TestNewPasswordResetTokenIsRandomAndNotStoredDirectly(t *testing.T) {
	first, err := newPasswordResetToken()
	if err != nil {
		t.Fatal(err)
	}
	second, err := newPasswordResetToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 || first == second {
		t.Fatalf("expected distinct 256-bit reset tokens, got lengths %d and %d", len(first), len(second))
	}
	if hashPasswordResetToken(first) == first {
		t.Fatal("reset token must not be stored in plaintext")
	}
}

func TestPasswordResetFilterUsesHashAndExpiry(t *testing.T) {
	now := time.Date(2026, time.August, 18, 12, 0, 0, 0, time.UTC)
	filter := passwordResetFilter(" reset-token ", now)
	if filter["tokenHash"] != hashPasswordResetToken("reset-token") {
		t.Fatalf("unexpected token hash filter: %#v", filter)
	}
	expiresAt, ok := filter["expiresAt"].(bson.M)
	if !ok || !expiresAt["$gt"].(time.Time).Equal(now) {
		t.Fatalf("unexpected expiry filter: %#v", filter)
	}
}
