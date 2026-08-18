package ai

import (
	"strings"
	"testing"
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
