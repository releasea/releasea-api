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

func TestCheckTemplateRepoAvailabilityFallsBackWhenTokenIsForbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			http.Error(w, `{"message":"Resource not accessible by personal access token"}`, http.StatusForbidden)
			return
		}
		if r.URL.Path == "/repos/releasea/new-service" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/repos/releasea/templates" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"full_name":"releasea/templates"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	previous := githubAPIBaseURL
	githubAPIBaseURL = server.URL
	defer func() {
		githubAPIBaseURL = previous
	}()

	exists, err := (githubRuntime{}).CheckTemplateRepoAvailability(context.Background(), "limited-token", "releasea", "new-service")
	if err != nil {
		t.Fatalf("expected a new name to be available: %v", err)
	}
	if exists {
		t.Fatal("expected a new name to be reported as available")
	}

	exists, err = (githubRuntime{}).CheckTemplateRepoAvailability(context.Background(), "limited-token", "releasea", "templates")
	if err != nil {
		t.Fatalf("expected an existing public repository to be detected: %v", err)
	}
	if !exists {
		t.Fatal("expected an existing public repository to be reported as taken")
	}
}
