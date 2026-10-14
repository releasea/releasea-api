package github

import (
	"strings"
	"testing"
)

func TestResponseErrorExplainsRejectedGitHubToken(t *testing.T) {
	err := ResponseError(401, []byte(`{"message":"Bad credentials"}`), "Failed to validate GitHub credential")
	if err == nil {
		t.Fatal("expected an authentication error")
	}

	message := err.Error()
	for _, expected := range []string{
		"Failed to validate GitHub credential",
		"GitHub authentication failed (HTTP 401)",
		"invalid, expired, revoked, or malformed",
		"GitHub SCM credential",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected %q in error message %q", expected, message)
		}
	}
	if strings.Contains(message, "Bad credentials") {
		t.Fatalf("expected the generic upstream message to be replaced, got %q", message)
	}
}

func TestResponseErrorExplainsGitHubPermissionFailure(t *testing.T) {
	err := ResponseError(403, []byte(`{"message":"Resource not accessible by personal access token"}`), "Failed to create repository")
	if err == nil {
		t.Fatal("expected a permission error")
	}

	message := err.Error()
	for _, expected := range []string{
		"GitHub denied access (HTTP 403)",
		"Resource not accessible by personal access token",
		"token permissions",
		"SSO authorization",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("expected %q in error message %q", expected, message)
		}
	}
}

func TestResponseErrorIncludesOperationAndGitHubStatus(t *testing.T) {
	err := ResponseError(422, []byte(`{"message":"Validation Failed"}`), "Failed to create repository")
	if err == nil {
		t.Fatal("expected a GitHub API error")
	}

	expected := "Failed to create repository (GitHub HTTP 422): Validation Failed"
	if err.Error() != expected {
		t.Fatalf("expected %q, got %q", expected, err.Error())
	}
}
