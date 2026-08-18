package shared

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

const encryptedValuePrefix = "enc:v1:"

// EncryptSensitiveValue protects application-managed credentials before they
// are persisted. CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key.
func EncryptSensitiveValue(plaintext string) (string, error) {
	key, err := credentialEncryptionKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encryptedValuePrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func DecryptSensitiveValue(value string) (string, error) {
	if !strings.HasPrefix(value, encryptedValuePrefix) {
		return value, nil // Backward-compatible read path for legacy documents.
	}
	key, err := credentialEncryptionKey()
	if err != nil {
		return "", err
	}
	payload, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, encryptedValuePrefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted credential: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted credential is truncated")
	}
	plaintext, err := gcm.Open(nil, payload[:gcm.NonceSize()], payload[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt credential: %w", err)
	}
	return string(plaintext), nil
}

func DecryptCredentialDocument(doc bson.M, fields ...string) (bson.M, error) {
	if doc == nil {
		return bson.M{}, nil
	}
	out := bson.M{}
	for key, value := range doc {
		out[key] = value
	}
	for _, field := range fields {
		value := StringValue(out[field])
		if value == "" {
			continue
		}
		decrypted, err := DecryptSensitiveValue(value)
		if err != nil {
			return nil, err
		}
		out[field] = decrypted
	}
	return out, nil
}

func credentialEncryptionKey() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("CREDENTIAL_ENCRYPTION_KEY"))
	if raw == "" {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY is required to store credentials")
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("CREDENTIAL_ENCRYPTION_KEY must be a base64-encoded 32-byte key")
	}
	return key, nil
}
