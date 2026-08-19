package ai

import (
	"encoding/json"
	"strings"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestValidateProviderURL(t *testing.T) {
	tests := []struct {
		name, value string
		private     bool
		wantErr     bool
	}{
		{"openai", "https://api.openai.com/v1", false, false},
		{"local denied", "http://127.0.0.1:11434/v1", false, true},
		{"local allowed", "http://127.0.0.1:11434/v1", true, false},
		{"metadata denied", "http://169.254.169.254/latest", true, true},
		{"userinfo denied", "https://user:pass@example.com/v1", false, true},
		{"scheme denied", "file:///tmp/model", true, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := validateProviderURL(test.value, test.private)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateProviderURL() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestRedactSensitive(t *testing.T) {
	value := redactSensitive("Authorization: Bearer abc.def token=my-secret password=hunter2 sk-1234567890abcdef")
	for _, secret := range []string{"abc.def", "my-secret", "hunter2", "sk-1234567890abcdef"} {
		if strings.Contains(value, secret) {
			t.Fatalf("redacted output still contains %q: %s", secret, value)
		}
	}
}

func TestSanitizeValueDropsSensitiveKeys(t *testing.T) {
	input := map[string]interface{}{"name": "service", "apiKey": "secret", "nested": map[string]interface{}{"password": "hidden", "status": "ok"}}
	clean := sanitizeValue(input).(map[string]interface{})
	if _, ok := clean["apiKey"]; ok {
		t.Fatal("apiKey was not removed")
	}
	nested := clean["nested"].(map[string]interface{})
	if _, ok := nested["password"]; ok {
		t.Fatal("nested password was not removed")
	}
	if nested["status"] != "ok" {
		t.Fatal("safe field was removed")
	}
}

func TestLimitServiceContextKeepsValidBoundedJSON(t *testing.T) {
	value := serviceContext{
		ServiceID: "svc-1",
		Evidence: []evidence{
			{ID: "service-config", Type: "service", Label: "Service", Data: bson.M{"message": strings.Repeat("a", 8000)}},
			{ID: "runtime-logs", Type: "logs", Label: "Logs", Data: []string{strings.Repeat("b", 8000)}},
		},
	}
	limited := limitServiceContext(value, 1200)
	encoded := marshalContext(limited)
	if len(encoded) > 1200 {
		t.Fatalf("context exceeds limit: %d", len(encoded))
	}
	if !json.Valid([]byte(encoded)) {
		t.Fatalf("context is not valid JSON: %s", encoded)
	}
	if !limited.Truncated || len(limited.Evidence) == 0 {
		t.Fatalf("expected compacted evidence, got %#v", limited)
	}
}

func TestAvailableProviderDocumentExcludesConnectionSecrets(t *testing.T) {
	doc := bson.M{"id": "aip-1", "name": "Local", "type": "openai-compatible", "model": "llama", "default": true, "baseUrl": "http://private:11434/v1", "apiKey": "encrypted", "health": bson.M{"state": "healthy"}}
	available := availableProviderDocument(doc)
	for _, key := range []string{"apiKey", "baseUrl"} {
		if _, ok := available[key]; ok {
			t.Fatalf("available provider exposed %s", key)
		}
	}
}
