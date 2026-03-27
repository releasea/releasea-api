package registryproviders

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestValidateCredential(t *testing.T) {
	if err := ValidateCredential("docker"); err != nil {
		t.Fatalf("docker should be valid: %v", err)
	}
	if err := ValidateCredential(""); err != nil {
		t.Fatalf("empty provider should resolve to default docker: %v", err)
	}
	if err := ValidateCredential("quay"); err == nil {
		t.Fatalf("expected unsupported registry provider validation error")
	}
}

func TestResolveRuntimeHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/" {
			http.NotFound(w, r)
			return
		}
		username, password, ok := r.BasicAuth()
		if !ok || username != "releasea" || password != "secret" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	runtime, err := ResolveRuntime("docker")
	if err != nil {
		t.Fatalf("expected docker runtime: %v", err)
	}

	if err := runtime.HealthCheck(context.Background(), map[string]interface{}{
		"username":    "releasea",
		"password":    "secret",
		"registryUrl": server.URL,
	}); err != nil {
		t.Fatalf("expected registry healthcheck to succeed: %v", err)
	}
}
