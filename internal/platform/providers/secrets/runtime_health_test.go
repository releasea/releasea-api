package secretsproviders

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVaultRuntimeHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/auth/token/lookup-self" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("X-Vault-Token"); got != "vault-token" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{"id":"token"}}`))
	}))
	defer server.Close()

	provider := &Provider{
		ID:   "sp-1",
		Type: "vault",
		Config: map[string]interface{}{
			"address": server.URL,
			"token":   "vault-token",
		},
	}

	if err := (vaultRuntime{}).HealthCheck(context.Background(), provider); err != nil {
		t.Fatalf("expected vault healthcheck to succeed: %v", err)
	}
}
