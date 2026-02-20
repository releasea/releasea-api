package services

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
	"time"

	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	releaseaManagedBy         = "releasea-platform"
	releaseaManagedMarkerPath = ".releasea/managed.json"
)

type repoRef struct {
	Owner string
	Name  string
}

type githubError struct {
	Message string `json:"message"`
}

type githubContentResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type releaseaManagedMarker struct {
	ManagedBy string `json:"managedBy"`
	ProjectID string `json:"projectId"`
}

func resolveServiceScmToken(ctx context.Context, service bson.M, project bson.M, repoKey string) (string, string, error) {
	cred, err := resolveServiceScmCredential(ctx, service, project)
	if err != nil {
		return "", "", err
	}
	if cred == nil {
		return "", "", fmt.Errorf("SCM credential not found for repository %s", repoKey)
	}
	provider := strings.ToLower(shared.StringValue(cred["provider"]))
	if provider == "" {
		provider = "github"
	}
	if provider != "github" {
		return "", provider, nil
	}
	return strings.TrimSpace(shared.StringValue(cred["token"])), provider, nil
}

func resolveServiceScmCredential(ctx context.Context, service bson.M, project bson.M) (bson.M, error) {
	if id := strings.TrimSpace(shared.StringValue(service["scmCredentialId"])); id != "" {
		return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
	}
	if project != nil {
		if id := strings.TrimSpace(shared.StringValue(project["scmCredentialId"])); id != "" {
			return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
		}
	}
	return findLatestPlatformCredential(ctx, shared.ScmCredentialsCollection)
}

func findLatestPlatformCredential(ctx context.Context, collectionName string) (bson.M, error) {
	col := shared.Collection(collectionName)
	filter := bson.M{"scope": "platform"}
	opts := options.FindOne().SetSort(bson.D{
		{Key: "updatedAt", Value: -1},
		{Key: "createdAt", Value: -1},
	})
	var result bson.M
	err := col.FindOne(ctx, filter, opts).Decode(&result)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, err
		}
		return nil, err
	}
	return result, nil
}

func parseGithubRepo(repoURL string) (repoRef, bool) {
	trimmed := strings.TrimSpace(repoURL)
	if trimmed == "" {
		return repoRef{}, false
	}

	if strings.HasPrefix(trimmed, "git@") {
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			return repoRef{}, false
		}
		host := strings.TrimPrefix(parts[0], "git@")
		if strings.ToLower(host) != "github.com" {
			return repoRef{}, false
		}
		path := strings.TrimSuffix(parts[1], ".git")
		return splitRepoPath(path)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return repoRef{}, false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "github.com" {
		return repoRef{}, false
	}
	path := strings.TrimSuffix(strings.Trim(parsed.Path, "/"), ".git")
	return splitRepoPath(path)
}

func splitRepoPath(path string) (repoRef, bool) {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 {
		return repoRef{}, false
	}
	return repoRef{Owner: segments[0], Name: segments[1]}, true
}

func deleteManagedGithubRepo(ctx context.Context, token string, repo repoRef, projectID string, allowWithoutMarker bool) (bool, error) {
	marker, exists, err := fetchReleaseaMarker(ctx, token, repo)
	if err != nil {
		return false, err
	}
	if !exists || marker.ManagedBy != releaseaManagedBy {
		if !allowWithoutMarker {
			return false, nil
		}
	} else if marker.ProjectID != "" && projectID != "" && marker.ProjectID != projectID {
		return false, nil
	}

	deleteURL := fmt.Sprintf("https://api.github.com/repos/%s/%s", repo.Owner, repo.Name)
	body, status, err := githubRequest(ctx, token, http.MethodDelete, deleteURL, nil)
	if err != nil {
		return false, fmt.Errorf("GitHub request failed: %w", err)
	}
	if status == http.StatusNotFound {
		return true, nil
	}
	if status != http.StatusNoContent && status != http.StatusAccepted {
		return false, githubResponseError(status, body, fmt.Sprintf("Failed to delete repository %s/%s", repo.Owner, repo.Name))
	}
	return true, nil
}

func fetchReleaseaMarker(ctx context.Context, token string, repo repoRef) (*releaseaManagedMarker, bool, error) {
	path := escapeGithubPath(releaseaManagedMarkerPath)
	contentURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s", repo.Owner, repo.Name, path)
	body, status, err := githubRequest(ctx, token, http.MethodGet, contentURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("GitHub request failed: %w", err)
	}
	if status == http.StatusNotFound {
		return nil, false, nil
	}
	if status < 200 || status >= 300 {
		return nil, false, githubResponseError(status, body, fmt.Sprintf("Failed to read repository %s/%s marker", repo.Owner, repo.Name))
	}

	var payload githubContentResponse
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
	var marker releaseaManagedMarker
	if err := json.Unmarshal(raw, &marker); err != nil {
		return nil, false, nil
	}
	return &marker, true, nil
}

func githubRequest(ctx context.Context, token, method, url string, payload interface{}) ([]byte, int, error) {
	var body io.Reader
	if payload != nil {
		rawBody, err := json.Marshal(payload)
		if err != nil {
			return nil, 0, err
		}
		body = bytes.NewReader(rawBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "releasea-api")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 20 * time.Second}
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

func githubResponseError(status int, body []byte, fallback string) error {
	if status >= 200 && status < 300 {
		return nil
	}
	msg := githubErrorMessage(body)
	if msg == "" {
		msg = fallback
	}
	return errors.New(msg)
}

func githubErrorMessage(body []byte) string {
	var ghErr githubError
	if err := json.Unmarshal(body, &ghErr); err == nil {
		if msg := strings.TrimSpace(ghErr.Message); msg != "" {
			return msg
		}
	}
	return ""
}

func escapeGithubPath(value string) string {
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}
