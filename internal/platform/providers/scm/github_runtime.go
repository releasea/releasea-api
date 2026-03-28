package scmproviders

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"strings"
	"time"

	scmmodels "releaseaapi/internal/features/scm/models"
	httpclient "releaseaapi/internal/platform/http/client"
	gh "releaseaapi/internal/platform/integrations/github"
	platformmodels "releaseaapi/internal/platform/models"

	yaml "gopkg.in/yaml.v3"
)

const (
	templateSourceOwner = "releasea"
	templateSourceRepo  = "templates"
	templateManifestYML = "releasea.yaml"
)

var githubAPIBaseURL = "https://api.github.com"

type githubRuntime struct{}

func (githubRuntime) ID() string {
	return "github"
}

func (githubRuntime) HealthCheck(ctx context.Context, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("github token missing")
	}
	client := httpclient.New(15 * time.Second)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, githubAPIBaseURL+"/user", nil)
	if err != nil {
		return fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to validate GitHub credential"); err != nil {
		return err
	}
	return nil
}

func (githubRuntime) CheckTemplateRepoAvailability(ctx context.Context, token, owner, name string) (bool, error) {
	repoURL := fmt.Sprintf(
		"%s/repos/%s/%s",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(name),
	)
	client := httpclient.New(15 * time.Second)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, repoURL, nil)
	if err != nil {
		return false, fmt.Errorf("GitHub request failed: %w", err)
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if err := gh.ResponseError(status, body, "Failed to check repository availability"); err != nil {
		return false, err
	}
	return true, nil
}

func (githubRuntime) CreateTemplateRepo(ctx context.Context, token string, payload scmmodels.TemplateRepoRequest) (*scmmodels.TemplateRepoResponse, error) {
	client := httpclient.New(20 * time.Second)

	templateInfo, err := fetchGithubRepoInfo(ctx, client, token, payload.TemplateOwner, payload.TemplateRepo)
	if err != nil {
		return nil, err
	}

	branch := strings.TrimSpace(templateInfo.DefaultBranch)
	if branch == "" {
		branch = "main"
	}

	treeEntries, err := fetchGithubTree(ctx, client, token, payload.TemplateOwner, payload.TemplateRepo, branch)
	if err != nil {
		return nil, err
	}

	templatePath := strings.Trim(strings.TrimSpace(payload.TemplatePath), "/")
	prefix := ""
	if templatePath != "" {
		prefix = templatePath + "/"
	}
	var files []scmmodels.GitHubTreeEntry
	for _, entry := range treeEntries {
		if entry.Type != "blob" {
			continue
		}
		if prefix == "" || strings.HasPrefix(entry.Path, prefix) {
			files = append(files, entry)
		}
	}
	if len(files) == 0 {
		return nil, errors.New("template repository is empty")
	}
	manifest, err := loadTemplateManifest(ctx, client, token, payload.TemplateOwner, payload.TemplateRepo, files, prefix)
	if err != nil {
		return nil, err
	}
	if err := validateTemplateManifest(manifest, templatePath); err != nil {
		return nil, err
	}

	repo, err := createGithubRepo(ctx, client, token, payload)
	if err != nil {
		return nil, err
	}

	repoOwner := payload.Owner
	if repo.Owner.Login != "" {
		repoOwner = repo.Owner.Login
	}
	repoBranch := strings.TrimSpace(repo.DefaultBranch)
	if repoBranch == "" {
		repoBranch = "main"
	}

	filesToCreate := make([]scmmodels.TemplateFileContent, 0, len(files)+1)
	createdPaths := make(map[string]struct{}, len(files)+1)
	for _, entry := range files {
		blob, err := fetchGithubBlob(ctx, client, token, payload.TemplateOwner, payload.TemplateRepo, entry.Sha)
		if err != nil {
			return nil, err
		}
		if strings.ToLower(blob.Encoding) != "base64" {
			return nil, fmt.Errorf("unsupported blob encoding: %s", blob.Encoding)
		}
		content := strings.ReplaceAll(blob.Content, "\n", "")
		content = strings.ReplaceAll(content, "\r", "")
		targetPath := strings.TrimPrefix(entry.Path, prefix)
		if targetPath == "" {
			continue
		}
		if _, exists := createdPaths[targetPath]; exists {
			continue
		}
		createdPaths[targetPath] = struct{}{}
		mode := strings.TrimSpace(entry.Mode)
		if mode == "" {
			mode = "100644"
		}
		filesToCreate = append(filesToCreate, scmmodels.TemplateFileContent{
			Path:          targetPath,
			Mode:          mode,
			ContentBase64: content,
		})
	}

	if _, exists := createdPaths[".releasea/managed.json"]; !exists {
		markerContent, err := buildReleaseaManagedMarker(payload)
		if err == nil {
			filesToCreate = append(filesToCreate, scmmodels.TemplateFileContent{
				Path:          ".releasea/managed.json",
				Mode:          "100644",
				ContentBase64: markerContent,
			})
		}
	}

	if err := createGithubInitialCommit(ctx, client, token, repoOwner, payload.Name, repoBranch, filesToCreate, payload); err != nil {
		_ = deleteGithubRepo(ctx, client, token, repoOwner, payload.Name)
		return nil, err
	}

	return repo, nil
}

func (githubRuntime) ListCommits(ctx context.Context, token, owner, repo, branch string) ([]scmmodels.CommitEntry, error) {
	client := httpclient.New(15 * time.Second)
	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/commits?sha=%s&per_page=20",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.QueryEscape(branch),
	)

	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to fetch commits"); err != nil {
		return nil, err
	}

	var source []scmmodels.GitHubCommitListEntry
	if err := json.Unmarshal(body, &source); err != nil {
		return nil, errors.New("failed to parse commits response")
	}

	commits := make([]scmmodels.CommitEntry, 0, len(source))
	for _, commit := range source {
		message := commit.Commit.Message
		if idx := strings.Index(message, "\n"); idx > 0 {
			message = message[:idx]
		}
		commits = append(commits, scmmodels.CommitEntry{
			Sha:     commit.Sha,
			Message: message,
			Author:  commit.Commit.Author.Name,
			Date:    commit.Commit.Author.Date,
		})
	}
	return commits, nil
}

func (githubRuntime) ListCommitsByRepoURL(ctx context.Context, token, repoURL, branch string) ([]scmmodels.CommitEntry, error) {
	repo, ok := gh.ParseRepo(repoURL)
	if !ok {
		return nil, errors.New("repository URL is not a valid GitHub repository")
	}
	return (githubRuntime{}).ListCommits(ctx, token, repo.Owner, repo.Name, branch)
}

func (githubRuntime) LatestCommitSHA(ctx context.Context, token, repoURL, branch string) (string, error) {
	repo, ok := gh.ParseRepo(repoURL)
	if !ok {
		return "", errors.New("repository URL is not a valid GitHub repository")
	}

	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/commits/%s",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(repo.Owner),
		url.PathEscape(repo.Name),
		url.PathEscape(branch),
	)
	body, status, err := gh.Request(ctx, token, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to fetch latest commit"); err != nil {
		return "", err
	}

	var response platformmodels.GitHubCommitHeadResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse latest commit response: %w", err)
	}

	sha := strings.TrimSpace(response.Sha)
	if sha == "" {
		return "", errors.New("latest commit SHA missing")
	}
	return sha, nil
}

func (githubRuntime) DeleteManagedRepo(ctx context.Context, token, repoURL, projectID string, allowWithoutMarker bool) (bool, error) {
	repo, ok := gh.ParseRepo(repoURL)
	if !ok {
		return false, errors.New("repository URL is not a valid GitHub repository")
	}
	return gh.DeleteManagedRepo(ctx, token, repo, projectID, allowWithoutMarker)
}

func (githubRuntime) CreateDesiredStatePullRequest(
	ctx context.Context,
	token string,
	payload scmmodels.DesiredStatePullRequestRequest,
) (*scmmodels.DesiredStatePullRequestResponse, error) {
	repo, ok := gh.ParseRepo(payload.RepoURL)
	if !ok {
		return nil, errors.New("repository URL is not a valid GitHub repository")
	}

	client := httpclient.New(20 * time.Second)

	baseBranch := strings.TrimSpace(payload.BaseBranch)
	if baseBranch == "" {
		baseBranch = "main"
	}
	baseSHA, err := fetchGithubBranchSHA(ctx, client, token, repo.Owner, repo.Name, baseBranch)
	if err != nil {
		return nil, err
	}

	if err := createGithubBranch(ctx, client, token, repo.Owner, repo.Name, payload.BranchName, baseSHA); err != nil {
		return nil, err
	}

	files := make([]scmmodels.DesiredStatePullRequestFile, 0, 1+len(payload.AdditionalFiles))
	files = append(files, scmmodels.DesiredStatePullRequestFile{
		Path:    payload.FilePath,
		Content: payload.Content,
	})
	files = append(files, payload.AdditionalFiles...)
	if err := putGithubPullRequestFiles(ctx, client, token, repo.Owner, repo.Name, payload, files); err != nil {
		return nil, err
	}

	return openGithubPullRequest(ctx, client, token, repo.Owner, repo.Name, payload)
}

func (githubRuntime) ReadFileContent(ctx context.Context, token, repoURL, path, ref string) (string, error) {
	repo, ok := gh.ParseRepo(repoURL)
	if !ok {
		return "", errors.New("repository URL is not a valid GitHub repository")
	}
	client := httpclient.New(15 * time.Second)
	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/contents/%s?ref=%s",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(repo.Owner),
		url.PathEscape(repo.Name),
		gh.EscapePath(path),
		url.QueryEscape(strings.TrimSpace(ref)),
	)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if status == http.StatusNotFound {
		return "", fs.ErrNotExist
	}
	if err := gh.ResponseError(status, body, "Failed to read repository file"); err != nil {
		return "", err
	}

	var response scmmodels.GitHubBlobInfo
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse repository file content: %w", err)
	}
	decoded, err := decodeGithubBlobContent(&response)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func loadTemplateManifest(
	ctx context.Context,
	client *http.Client,
	token, owner, repo string,
	files []scmmodels.GitHubTreeEntry,
	prefix string,
) (*scmmodels.TemplateManifest, error) {
	manifestPath := templateManifestYML
	if prefix != "" {
		manifestPath = prefix + templateManifestYML
	}
	var manifestEntry *scmmodels.GitHubTreeEntry
	for i := range files {
		if files[i].Path == manifestPath {
			manifestEntry = &files[i]
			break
		}
	}
	if manifestEntry == nil {
		return nil, fmt.Errorf("template manifest not found: %s", manifestPath)
	}

	blob, err := fetchGithubBlob(ctx, client, token, owner, repo, manifestEntry.Sha)
	if err != nil {
		return nil, err
	}
	content, err := decodeGithubBlobContent(blob)
	if err != nil {
		return nil, fmt.Errorf("failed to decode template manifest: %w", err)
	}

	var manifest scmmodels.TemplateManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("invalid template manifest YAML: %w", err)
	}
	return &manifest, nil
}

func validateTemplateManifest(manifest *scmmodels.TemplateManifest, templatePath string) error {
	if manifest == nil {
		return errors.New("template manifest missing")
	}
	if strings.TrimSpace(manifest.Kind) != "ReleaseaTemplate" {
		return fmt.Errorf("invalid template manifest kind: %s", strings.TrimSpace(manifest.Kind))
	}
	if strings.TrimSpace(manifest.Source.Owner) != templateSourceOwner || strings.TrimSpace(manifest.Source.Repo) != templateSourceRepo {
		return fmt.Errorf("template source must be %s/%s", templateSourceOwner, templateSourceRepo)
	}
	if strings.Trim(strings.TrimSpace(manifest.Source.Path), "/") != strings.Trim(strings.TrimSpace(templatePath), "/") {
		return fmt.Errorf("template manifest path mismatch for %s", strings.TrimSpace(templatePath))
	}
	return nil
}

func fetchGithubBranchSHA(ctx context.Context, client *http.Client, token, owner, repo, branch string) (string, error) {
	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/git/ref/heads/%s",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
		url.PathEscape(branch),
	)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to load base branch reference"); err != nil {
		return "", err
	}

	var ref scmmodels.GitHubRefInfo
	if err := json.Unmarshal(body, &ref); err != nil {
		return "", fmt.Errorf("failed to parse branch reference: %w", err)
	}
	sha := strings.TrimSpace(ref.Object.Sha)
	if sha == "" {
		return "", errors.New("base branch SHA missing")
	}
	return sha, nil
}

func createGithubBranch(ctx context.Context, client *http.Client, token, owner, repo, branchName, sha string) error {
	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/git/refs",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
	)
	payload := map[string]string{
		"ref": "refs/heads/" + strings.TrimSpace(branchName),
		"sha": strings.TrimSpace(sha),
	}
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodPost, apiURL, payload)
	if err != nil {
		return fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to create GitOps branch"); err != nil {
		return err
	}
	return nil
}

func fetchGithubContentSHA(ctx context.Context, client *http.Client, token, owner, repo, path, branch string) (string, error) {
	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/contents/%s?ref=%s",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
		gh.EscapePath(path),
		url.QueryEscape(branch),
	)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if status == http.StatusNotFound {
		return "", nil
	}
	if err := gh.ResponseError(status, body, "Failed to inspect desired state file"); err != nil {
		return "", err
	}

	var item scmmodels.GitHubContentItemResponse
	if err := json.Unmarshal(body, &item); err != nil {
		return "", fmt.Errorf("failed to parse desired state file metadata: %w", err)
	}
	return strings.TrimSpace(item.Sha), nil
}

func putGithubFileContent(
	ctx context.Context,
	client *http.Client,
	token, owner, repo string,
	payload scmmodels.DesiredStatePullRequestRequest,
	existingSHA string,
) error {
	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/contents/%s",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
		gh.EscapePath(payload.FilePath),
	)
	requestPayload := map[string]string{
		"message": payload.CommitMessage,
		"content": base64.StdEncoding.EncodeToString([]byte(payload.Content)),
		"branch":  payload.BranchName,
	}
	if strings.TrimSpace(existingSHA) != "" {
		requestPayload["sha"] = strings.TrimSpace(existingSHA)
	}

	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodPut, apiURL, requestPayload)
	if err != nil {
		return fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to write desired state file"); err != nil {
		return err
	}
	return nil
}

func putGithubPullRequestFiles(
	ctx context.Context,
	client *http.Client,
	token, owner, repo string,
	payload scmmodels.DesiredStatePullRequestRequest,
	files []scmmodels.DesiredStatePullRequestFile,
) error {
	for _, file := range files {
		path := strings.TrimSpace(file.Path)
		if path == "" {
			continue
		}
		existingSHA, err := fetchGithubContentSHA(ctx, client, token, owner, repo, path, payload.BranchName)
		if err != nil {
			return err
		}
		if err := putGithubFileContent(ctx, client, token, owner, repo, scmmodels.DesiredStatePullRequestRequest{
			FilePath:      path,
			Content:       file.Content,
			CommitMessage: payload.CommitMessage,
			BranchName:    payload.BranchName,
		}, existingSHA); err != nil {
			return err
		}
	}
	return nil
}

func openGithubPullRequest(
	ctx context.Context,
	client *http.Client,
	token, owner, repo string,
	payload scmmodels.DesiredStatePullRequestRequest,
) (*scmmodels.DesiredStatePullRequestResponse, error) {
	apiURL := fmt.Sprintf(
		"%s/repos/%s/%s/pulls",
		strings.TrimRight(githubAPIBaseURL, "/"),
		url.PathEscape(owner),
		url.PathEscape(repo),
	)
	requestPayload := map[string]string{
		"title": payload.Title,
		"head":  payload.BranchName,
		"base":  payload.BaseBranch,
		"body":  payload.Body,
	}
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodPost, apiURL, requestPayload)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to open GitOps pull request"); err != nil {
		return nil, err
	}

	var pr scmmodels.GitHubPullRequestResponse
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse pull request response: %w", err)
	}
	return &scmmodels.DesiredStatePullRequestResponse{
		URL:        strings.TrimSpace(pr.HTMLURL),
		Number:     pr.Number,
		BaseBranch: payload.BaseBranch,
		BranchName: payload.BranchName,
		FilePath:   payload.FilePath,
		FilePaths:  collectPullRequestFilePaths(payload),
		Title:      payload.Title,
	}, nil
}

func collectPullRequestFilePaths(payload scmmodels.DesiredStatePullRequestRequest) []string {
	paths := make([]string, 0, 1+len(payload.AdditionalFiles))
	if trimmed := strings.TrimSpace(payload.FilePath); trimmed != "" {
		paths = append(paths, trimmed)
	}
	for _, file := range payload.AdditionalFiles {
		if trimmed := strings.TrimSpace(file.Path); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

func fetchGithubRepoInfo(ctx context.Context, client *http.Client, token, owner, repo string) (*scmmodels.GitHubRepoInfo, error) {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to load template repository"); err != nil {
		return nil, err
	}

	var info scmmodels.GitHubRepoInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, errors.New("failed to parse template repository")
	}
	return &info, nil
}

func fetchGithubTree(ctx context.Context, client *http.Client, token, owner, repo, branch string) ([]scmmodels.GitHubTreeEntry, error) {
	refURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs/heads/%s", owner, repo, url.PathEscape(branch))
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, refURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to resolve template branch"); err != nil {
		return nil, err
	}

	var ref scmmodels.GitHubRefInfo
	if err := json.Unmarshal(body, &ref); err != nil {
		return nil, errors.New("failed to parse template branch")
	}
	if ref.Object.Sha == "" {
		return nil, errors.New("template branch SHA missing")
	}

	commitURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/commits/%s", owner, repo, ref.Object.Sha)
	body, status, err = gh.RequestWithClient(ctx, client, token, http.MethodGet, commitURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to load template commit"); err != nil {
		return nil, err
	}

	var commit scmmodels.GitHubCommitInfo
	if err := json.Unmarshal(body, &commit); err != nil {
		return nil, errors.New("failed to parse template commit")
	}
	if commit.Tree.Sha == "" {
		return nil, errors.New("template tree SHA missing")
	}

	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, commit.Tree.Sha)
	body, status, err = gh.RequestWithClient(ctx, client, token, http.MethodGet, treeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to load template tree"); err != nil {
		return nil, err
	}

	var tree scmmodels.GitHubTreeInfo
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, errors.New("failed to parse template tree")
	}
	if tree.Truncated {
		return nil, errors.New("template repository is too large to copy")
	}
	return tree.Tree, nil
}

func fetchGithubBlob(ctx context.Context, client *http.Client, token, owner, repo, sha string) (*scmmodels.GitHubBlobInfo, error) {
	blobURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/blobs/%s", owner, repo, sha)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, blobURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to load template file"); err != nil {
		return nil, err
	}

	var blob scmmodels.GitHubBlobInfo
	if err := json.Unmarshal(body, &blob); err != nil {
		return nil, errors.New("failed to parse template file")
	}
	return &blob, nil
}

func createGithubRepo(ctx context.Context, client *http.Client, token string, payload scmmodels.TemplateRepoRequest) (*scmmodels.TemplateRepoResponse, error) {
	userURL := "https://api.github.com/user"
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodGet, userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to resolve GitHub user"); err != nil {
		return nil, err
	}

	var user scmmodels.GitHubUserInfo
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, errors.New("failed to parse GitHub user")
	}

	endpoint := "https://api.github.com/user/repos"
	if payload.Owner != "" && !strings.EqualFold(payload.Owner, user.Login) {
		endpoint = fmt.Sprintf("https://api.github.com/orgs/%s/repos", payload.Owner)
	}

	requestBody := map[string]interface{}{
		"name":      payload.Name,
		"private":   payload.Private,
		"auto_init": true,
	}
	if payload.Description != "" {
		requestBody["description"] = payload.Description
	}

	body, status, err = gh.RequestWithClient(ctx, client, token, http.MethodPost, endpoint, requestBody)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to create repository"); err != nil {
		return nil, err
	}

	var repo scmmodels.TemplateRepoResponse
	if err := json.Unmarshal(body, &repo); err != nil {
		return nil, errors.New("failed to parse repository response")
	}
	return &repo, nil
}

func createGithubInitialCommit(
	ctx context.Context,
	client *http.Client,
	token, owner, repo, branch string,
	files []scmmodels.TemplateFileContent,
	payload scmmodels.TemplateRepoRequest,
) error {
	if len(files) == 0 {
		return errors.New("template repository is empty")
	}

	treeEntries := make([]scmmodels.GitHubCreateTreeEntry, 0, len(files))
	for _, file := range files {
		blobSha, err := createGithubBlob(ctx, client, token, owner, repo, file.ContentBase64)
		if err != nil {
			return err
		}
		mode := strings.TrimSpace(file.Mode)
		if mode == "" {
			mode = "100644"
		}
		treeEntries = append(treeEntries, scmmodels.GitHubCreateTreeEntry{
			Path: file.Path,
			Mode: mode,
			Type: "blob",
			Sha:  blobSha,
		})
	}

	treeSha, err := createGithubTree(ctx, client, token, owner, repo, treeEntries)
	if err != nil {
		return err
	}

	message := fmt.Sprintf("Initial commit from template %s/%s", payload.TemplateOwner, payload.TemplateRepo)
	commitSha, err := createGithubCommit(ctx, client, token, owner, repo, message, treeSha)
	if err != nil {
		return err
	}

	return updateGithubBranchRef(ctx, client, token, owner, repo, branch, commitSha)
}

func createGithubBlob(ctx context.Context, client *http.Client, token, owner, repo, contentBase64 string) (string, error) {
	blobURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/blobs", owner, repo)
	requestBody := map[string]interface{}{
		"content":  contentBase64,
		"encoding": "base64",
	}
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodPost, blobURL, requestBody)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to create template blob"); err != nil {
		return "", err
	}

	var blob scmmodels.GitHubCreateBlobResponse
	if err := json.Unmarshal(body, &blob); err != nil {
		return "", errors.New("failed to parse created blob")
	}
	if strings.TrimSpace(blob.Sha) == "" {
		return "", errors.New("created blob SHA missing")
	}
	return blob.Sha, nil
}

func createGithubTree(ctx context.Context, client *http.Client, token, owner, repo string, entries []scmmodels.GitHubCreateTreeEntry) (string, error) {
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees", owner, repo)
	requestBody := map[string]interface{}{
		"tree": entries,
	}
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodPost, treeURL, requestBody)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to create template tree"); err != nil {
		return "", err
	}

	var tree scmmodels.GitHubCreateTreeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return "", errors.New("failed to parse created tree")
	}
	if strings.TrimSpace(tree.Sha) == "" {
		return "", errors.New("created tree SHA missing")
	}
	return tree.Sha, nil
}

func createGithubCommit(ctx context.Context, client *http.Client, token, owner, repo, message, treeSha string) (string, error) {
	commitURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/commits", owner, repo)
	botIdentity := map[string]string{
		"name":  "releasea-bot",
		"email": "releasea-bot@releasea.io",
	}
	requestBody := map[string]interface{}{
		"message":   message,
		"tree":      treeSha,
		"parents":   []string{},
		"author":    botIdentity,
		"committer": botIdentity,
	}
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodPost, commitURL, requestBody)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to create initial commit"); err != nil {
		return "", err
	}

	var commit scmmodels.GitHubCreateCommitResponse
	if err := json.Unmarshal(body, &commit); err != nil {
		return "", errors.New("failed to parse created commit")
	}
	if strings.TrimSpace(commit.Sha) == "" {
		return "", errors.New("created commit SHA missing")
	}
	return commit.Sha, nil
}

func updateGithubBranchRef(ctx context.Context, client *http.Client, token, owner, repo, branch, sha string) error {
	updateURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/git/refs/heads/%s",
		owner,
		repo,
		url.PathEscape(branch),
	)
	updateBody := map[string]interface{}{
		"sha":   sha,
		"force": true,
	}
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodPatch, updateURL, updateBody)
	if err != nil {
		return fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to update repository branch"); err != nil {
		return err
	}
	return nil
}

func deleteGithubRepo(ctx context.Context, client *http.Client, token, owner, repo string) error {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	body, status, err := gh.RequestWithClient(ctx, client, token, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || status == http.StatusNoContent || (status >= 200 && status < 300) {
		return nil
	}
	return gh.ResponseError(status, body, "Failed to clean up repository after template copy error")
}

func buildReleaseaManagedMarker(payload scmmodels.TemplateRepoRequest) (string, error) {
	marker := map[string]interface{}{
		"managedBy":     "releasea-platform",
		"projectId":     payload.ProjectID,
		"templateOwner": payload.TemplateOwner,
		"templateRepo":  payload.TemplateRepo,
		"templatePath":  payload.TemplatePath,
		"serviceName":   payload.Name,
		"createdAt":     time.Now().UTC().Format(time.RFC3339),
	}
	raw, err := json.Marshal(marker)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}

func decodeGithubBlobContent(blob *scmmodels.GitHubBlobInfo) ([]byte, error) {
	if blob == nil {
		return nil, errors.New("blob missing")
	}
	if strings.ToLower(strings.TrimSpace(blob.Encoding)) != "base64" {
		return nil, fmt.Errorf("unsupported blob encoding: %s", blob.Encoding)
	}
	content := strings.ReplaceAll(blob.Content, "\n", "")
	content = strings.ReplaceAll(content, "\r", "")
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, err
	}
	return decoded, nil
}
