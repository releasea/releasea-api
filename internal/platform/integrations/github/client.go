package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	httpclient "releaseaapi/internal/platform/http/client"
	httpheaders "releaseaapi/internal/platform/http/headers"
)

const (
	managedBy         = "releasea-platform"
	managedMarkerPath = ".releasea/managed.json"
)

type RepoRef struct {
	Owner string
	Name  string
}

type apiError struct {
	Message string `json:"message"`
}

type contentResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type managedMarker struct {
	ManagedBy string `json:"managedBy"`
	ProjectID string `json:"projectId"`
}

func ParseRepo(repoURL string) (RepoRef, bool) {
	trimmed := strings.TrimSpace(repoURL)
	if trimmed == "" {
		return RepoRef{}, false
	}

	if strings.HasPrefix(trimmed, "git@") {
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			return RepoRef{}, false
		}
		host := strings.TrimPrefix(parts[0], "git@")
		if strings.ToLower(host) != "github.com" {
			return RepoRef{}, false
		}
		path := strings.TrimSuffix(parts[1], ".git")
		return splitRepoPath(path)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return RepoRef{}, false
	}
	if strings.ToLower(parsed.Hostname()) != "github.com" {
		return RepoRef{}, false
	}
	path := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	return splitRepoPath(path)
}

func splitRepoPath(path string) (RepoRef, bool) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 {
		return RepoRef{}, false
	}
	return RepoRef{Owner: segments[0], Name: segments[1]}, true
}

func DeleteManagedRepo(ctx context.Context, token string, repo RepoRef, projectID string, allowWithoutMarker bool) (bool, error) {
	marker, exists, err := fetchManagedMarker(ctx, token, repo)
	if err != nil {
		return false, err
	}
	if !exists || marker.ManagedBy != managedBy {
		if !allowWithoutMarker {
			return false, nil
		}
	} else if marker.ProjectID != "" && projectID != "" && marker.ProjectID != projectID {
		return false, nil
	}

	deleteURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", repo.Owner, repo.Name)
	body, status, err := Request(ctx, token, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return false, fmt.Errorf("GitHub request failed: %w", err)
	}
	if status == http.StatusNotFound {
		return true, nil
	}
	if status != http.StatusNoContent && status != http.StatusAccepted {
		return false, ResponseError(status, body, fmt.Sprintf("Failed to delete repository %s/%s", repo.Owner, repo.Name))
	}
	return true, nil
}

func fetchManagedMarker(ctx context.Context, token string, repo RepoRef) (*managedMarker, bool, error) {
	path := EscapePath(managedMarkerPath)
	contentURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", repo.Owner, repo.Name, path)
	body, status, err := Request(ctx, token, http.MethodGet, contentURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("GitHub request failed: %w", err)
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	if status < 200 || status >= 300 {
		return nil, false, ResponseError(status, body, fmt.Sprintf("Failed to read repository %s/%s marker", repo.Owner, repo.Name))
	}

	var payload contentResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, false, nil
	}
	if strings.ToLower(payload.Encoding) != "base64" {
		return nil, false, nil
	}
	content := strings.ReplaceAll(payload.Content, "\n", "")
	content = strings.ReplaceAll(content, "\r", "")
	raw, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, false, nil
	}
	var marker managedMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, false, nil
	}
	return &marker, true, nil
}

func Request(ctx context.Context, token, method, endpoint string, payload interface{}) ([]byte, int, error) {
	client := httpclient.Default()
	return RequestWithClient(ctx, client, token, method, endpoint, payload)
}

func RequestWithClient(ctx context.Context, client *http.Client, token, method, endpoint string, payload interface{}) ([]byte, int, error) {
	if client == nil {
		client = httpclient.Default()
	}

	var body io.Reader
	if payload != nil {
		rawBody, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(rawBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, 0, err
	}
	httpheaders.ApplyGitHubRequest(req, token, payload != nil)

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return bodyBytes, resp.StatusCode, nil
}

func ResponseError(status int, body []byte, fallback string) error {
	if status >= 200 && status < 300 {
		return nil
	}
	msg := ErrorMessage(body)
	if msg == "" {
		msg = fallback
	}
	return errors.New(msg)
}

func ErrorMessage(body []byte) string {
	var ghErr apiError
	if err := json.Unmarshal(body, &ghErr); err == nil {
		if msg := strings.TrimSpace(ghErr.Message); msg != "" {
			return msg
		}
	}
	return ""
}

func EscapePath(value string) string {
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
