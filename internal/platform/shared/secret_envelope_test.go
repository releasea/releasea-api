package shared

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestSensitiveValueEnvelopeRoundTrip(t *testing.T) {
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	encrypted, err := EncryptSensitiveValue("top-secret")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted == "top-secret" || !strings.HasPrefix(encrypted, encryptedValuePrefix) {
		t.Fatalf("secret was not enveloped: %q", encrypted)
	}
	decrypted, err := DecryptSensitiveValue(encrypted)
	if err != nil || decrypted != "top-secret" {
		t.Fatalf("unexpected round trip: value=%q err=%v", decrypted, err)
	}
}

func TestSensitiveValueEnvelopeRequiresKeyForWrites(t *testing.T) {
	t.Setenv("CREDENTIAL_ENCRYPTION_KEY", "")
	if _, err := EncryptSensitiveValue("top-secret"); err == nil {
		t.Fatal("expected missing encryption key to reject credential write")
	}
	legacy, err := DecryptSensitiveValue("legacy-plaintext")
	if err != nil || legacy != "legacy-plaintext" {
		t.Fatalf("legacy read path failed: value=%q err=%v", legacy, err)
	}
}
