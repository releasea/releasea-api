package scmproviders

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGitHubRuntimeHealthCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"login":"releasea"}`))
	}))
	defer server.Close()

	previous := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() {
		githubAPIBaseURL = previous
	}()

	if err := (githubRuntime{}).HealthCheck(context.Background(), "test-token"); err != nil {
		t.Fatalf("expected GitHub healthcheck to succeed: %v", err)
	}
}
