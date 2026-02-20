package scm

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

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/yaml.v3"
)

const (
	templateSourceOwner = "releasea"
	templateSourceRepo  = "templates"
	templateManifestYML = "releasea.yaml"
)

type templateRepoRequest struct {
	ScmCredentialID string `json:"scmCredentialId"`
	ProjectID       string `json:"projectId"`
	TemplateOwner   string `json:"templateOwner"`
	TemplateRepo    string `json:"templateRepo"`
	TemplatePath    string `json:"templatePath"`
	Owner           string `json:"owner"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Private         bool   `json:"private"`
}

type templateRepoResponse struct {
	ID            int    `json:"id"`
	FullName      string `json:"full_name"`
	HTMLURL       string `json:"html_url"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Owner         struct {
		Login string `json:"login"`
	} `json:"owner"`
}

type githubError struct {
	Message string `json:"message"`
}

type githubRepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

type githubUserInfo struct {
	Login string `json:"login"`
}

type githubRefInfo struct {
	Object struct {
		Sha string `json:"sha"`
	} `json:"object"`
}

type githubCommitInfo struct {
	Tree struct {
		Sha string `json:"sha"`
	} `json:"tree"`
}

type githubTreeInfo struct {
	Tree      []githubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

type githubTreeEntry struct {
	Path string `json:"path"`
	Sha  string `json:"sha"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

type githubBlobInfo struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type githubCreateBlobResponse struct {
	Sha string `json:"sha"`
}

type githubCreateTreeResponse struct {
	Sha string `json:"sha"`
}

type githubCreateCommitResponse struct {
	Sha string `json:"sha"`
}

type githubCreateTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	Sha  string `json:"sha"`
}

type templateFileContent struct {
	Path          string
	Mode          string
	ContentBase64 string
}

type templateManifest struct {
	APIVersion   string `yaml:"apiVersion"`
	Kind         string `yaml:"kind"`
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	TemplateType string `yaml:"templateType"`
	Source       struct {
		Owner string `yaml:"owner"`
		Repo  string `yaml:"repo"`
		Path  string `yaml:"path"`
	} `yaml:"source"`
}

func CheckTemplateRepoAvailability(c *gin.Context) {
	owner := strings.TrimSpace(c.Query("owner"))
	name := strings.TrimSpace(c.Query("name"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	scmCredentialID := strings.TrimSpace(c.Query("scmCredentialId"))

	if owner == "" || name == "" {
		shared.RespondError(c, http.StatusBadRequest, "owner and name are required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	scmCred, err := resolveScmCredential(ctx, scmCredentialID, projectID)
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "SCM credential not found")
		return
	}

	provider := strings.ToLower(shared.StringValue(scmCred["provider"]))
	if provider != "" && provider != "github" {
		shared.RespondError(c, http.StatusBadRequest, "SCM credential must be GitHub")
		return
	}

	token := strings.TrimSpace(shared.StringValue(scmCred["token"]))
	if token == "" {
		shared.RespondError(c, http.StatusBadRequest, "SCM credential missing token")
		return
	}

	repoURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s",
		url.PathEscape(owner),
		url.PathEscape(name),
	)
	client := &http.Client{Timeout: 15 * time.Second}
	body, status, err := githubRequest(ctx, client, token, http.MethodGet, repoURL, nil)
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, "Failed to check repository availability")
		return
	}

	response := gin.H{
		"owner":    owner,
		"name":     name,
		"fullName": fmt.Sprintf("%s/%s", owner, name),
	}
	if status == http.StatusNotFound {
		response["exists"] = false
		c.JSON(http.StatusOK, response)
		return
	}
	if status == http.StatusOK {
		response["exists"] = true
		c.JSON(http.StatusOK, response)
		return
	}

	if err := githubResponseError(status, body, "Failed to check repository availability"); err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}
	shared.RespondError(c, http.StatusBadGateway, "Failed to check repository availability")
}

func CreateTemplateRepo(c *gin.Context) {
	var payload templateRepoRequest
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	payload.TemplateOwner = strings.TrimSpace(payload.TemplateOwner)
	payload.TemplateRepo = strings.TrimSpace(payload.TemplateRepo)
	payload.TemplatePath = strings.TrimSpace(payload.TemplatePath)
	payload.Owner = strings.TrimSpace(payload.Owner)
	payload.Name = strings.TrimSpace(payload.Name)
	payload.Description = strings.TrimSpace(payload.Description)

	if payload.TemplatePath == "" {
		shared.RespondError(c, http.StatusBadRequest, "Template path required")
		return
	}
	if payload.Owner == "" || payload.Name == "" {
		shared.RespondError(c, http.StatusBadRequest, "New repository owner and name required")
		return
	}
	payload.TemplateOwner = templateSourceOwner
	payload.TemplateRepo = templateSourceRepo

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	scmCred, err := resolveScmCredential(ctx, payload.ScmCredentialID, payload.ProjectID)
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "SCM credential not found")
		return
	}

	provider := strings.ToLower(shared.StringValue(scmCred["provider"]))
	if provider != "" && provider != "github" {
		shared.RespondError(c, http.StatusBadRequest, "SCM credential must be GitHub")
		return
	}

	token := strings.TrimSpace(shared.StringValue(scmCred["token"]))
	if token == "" {
		shared.RespondError(c, http.StatusBadRequest, "SCM credential missing token")
		return
	}

	repo, err := createTemplateRepoFromPath(ctx, token, payload)
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}
	c.JSON(http.StatusOK, repo)
}

func resolveScmCredential(ctx context.Context, scmCredentialID string, projectID string) (bson.M, error) {
	if strings.TrimSpace(scmCredentialID) != "" {
		return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": scmCredentialID})
	}
	if strings.TrimSpace(projectID) != "" {
		project, err := shared.FindOne(ctx, shared.Collection(shared.ProjectsCollection), bson.M{"id": projectID})
		if err == nil && project != nil {
			if id := shared.StringValue(project["scmCredentialId"]); id != "" {
				return shared.FindOne(ctx, shared.Collection(shared.ScmCredentialsCollection), bson.M{"id": id})
			}
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

func createTemplateRepoFromPath(ctx context.Context, token string, payload templateRepoRequest) (*templateRepoResponse, error) {
	client := &http.Client{Timeout: 20 * time.Second}

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
	var files []githubTreeEntry
	for _, entry := range treeEntries {
		if entry.Type != "blob" {
			continue
		}
		if prefix == "" || strings.HasPrefix(entry.Path, prefix) {
			files = append(files, entry)
		}
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("Template repository is empty")
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

	filesToCreate := make([]templateFileContent, 0, len(files)+1)
	createdPaths := make(map[string]struct{}, len(files)+1)
	for _, entry := range files {
		blob, err := fetchGithubBlob(ctx, client, token, payload.TemplateOwner, payload.TemplateRepo, entry.Sha)
		if err != nil {
			return nil, err
		}
		if strings.ToLower(blob.Encoding) != "base64" {
			return nil, fmt.Errorf("Unsupported blob encoding: %s", blob.Encoding)
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
		filesToCreate = append(filesToCreate, templateFileContent{
			Path:          targetPath,
			Mode:          mode,
			ContentBase64: content,
		})
	}

	if _, exists := createdPaths[".releasea/managed.json"]; !exists {
		markerContent, err := buildReleaseaManagedMarker(payload)
		if err == nil {
			filesToCreate = append(filesToCreate, templateFileContent{
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

func loadTemplateManifest(
	ctx context.Context,
	client *http.Client,
	token, owner, repo string,
	files []githubTreeEntry,
	prefix string,
) (*templateManifest, error) {
	manifestPath := templateManifestYML
	if prefix != "" {
		manifestPath = prefix + templateManifestYML
	}
	var manifestEntry *githubTreeEntry
	for i := range files {
		if files[i].Path == manifestPath {
			manifestEntry = &files[i]
			break
		}
	}
	if manifestEntry == nil {
		return nil, fmt.Errorf("Template manifest not found: %s", manifestPath)
	}

	blob, err := fetchGithubBlob(ctx, client, token, owner, repo, manifestEntry.Sha)
	if err != nil {
		return nil, err
	}
	content, err := decodeGithubBlobContent(blob)
	if err != nil {
		return nil, fmt.Errorf("Failed to decode template manifest: %w", err)
	}

	var manifest templateManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return nil, fmt.Errorf("Invalid template manifest YAML: %w", err)
	}
	return &manifest, nil
}

func validateTemplateManifest(manifest *templateManifest, templatePath string) error {
	if manifest == nil {
		return errors.New("Template manifest missing")
	}
	if strings.TrimSpace(manifest.Kind) != "ReleaseaTemplate" {
		return fmt.Errorf("Invalid template manifest kind: %s", strings.TrimSpace(manifest.Kind))
	}
	if strings.TrimSpace(manifest.Source.Owner) != templateSourceOwner || strings.TrimSpace(manifest.Source.Repo) != templateSourceRepo {
		return fmt.Errorf("Template source must be %s/%s", templateSourceOwner, templateSourceRepo)
	}
	if strings.Trim(strings.TrimSpace(manifest.Source.Path), "/") != strings.Trim(strings.TrimSpace(templatePath), "/") {
		return fmt.Errorf("Template manifest path mismatch for %s", strings.TrimSpace(templatePath))
	}
	return nil
}

func fetchGithubRepoInfo(ctx context.Context, client *http.Client, token, owner, repo string) (*githubRepoInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	body, status, err := githubRequest(ctx, client, token, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to load template repository"); err != nil {
		return nil, err
	}

	var info githubRepoInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, errors.New("Failed to parse template repository")
	}
	return &info, nil
}

func fetchGithubTree(ctx context.Context, client *http.Client, token, owner, repo, branch string) ([]githubTreeEntry, error) {
	refURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/refs/heads/%s", owner, repo, url.PathEscape(branch))
	body, status, err := githubRequest(ctx, client, token, http.MethodGet, refURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to resolve template branch"); err != nil {
		return nil, err
	}

	var ref githubRefInfo
	if err := json.Unmarshal(body, &ref); err != nil {
		return nil, errors.New("Failed to parse template branch")
	}
	if ref.Object.Sha == "" {
		return nil, errors.New("Template branch SHA missing")
	}

	commitURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/commits/%s", owner, repo, ref.Object.Sha)
	body, status, err = githubRequest(ctx, client, token, http.MethodGet, commitURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to load template commit"); err != nil {
		return nil, err
	}

	var commit githubCommitInfo
	if err := json.Unmarshal(body, &commit); err != nil {
		return nil, errors.New("Failed to parse template commit")
	}
	if commit.Tree.Sha == "" {
		return nil, errors.New("Template tree SHA missing")
	}

	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees/%s?recursive=1", owner, repo, commit.Tree.Sha)
	body, status, err = githubRequest(ctx, client, token, http.MethodGet, treeURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to load template tree"); err != nil {
		return nil, err
	}

	var tree githubTreeInfo
	if err := json.Unmarshal(body, &tree); err != nil {
		return nil, errors.New("Failed to parse template tree")
	}
	if tree.Truncated {
		return nil, errors.New("Template repository is too large to copy")
	}
	return tree.Tree, nil
}

func fetchGithubBlob(ctx context.Context, client *http.Client, token, owner, repo, sha string) (*githubBlobInfo, error) {
	blobURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/blobs/%s", owner, repo, sha)
	body, status, err := githubRequest(ctx, client, token, http.MethodGet, blobURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to load template file"); err != nil {
		return nil, err
	}

	var blob githubBlobInfo
	if err := json.Unmarshal(body, &blob); err != nil {
		return nil, errors.New("Failed to parse template file")
	}
	return &blob, nil
}

func createGithubRepo(ctx context.Context, client *http.Client, token string, payload templateRepoRequest) (*templateRepoResponse, error) {
	userURL := "https://api.github.com/user"
	body, status, err := githubRequest(ctx, client, token, http.MethodGet, userURL, nil)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to resolve GitHub user"); err != nil {
		return nil, err
	}

	var user githubUserInfo
	if err := json.Unmarshal(body, &user); err != nil {
		return nil, errors.New("Failed to parse GitHub user")
	}

	endpoint := "https://api.github.com/user/repos"
	if payload.Owner != "" && strings.ToLower(payload.Owner) != strings.ToLower(user.Login) {
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

	body, status, err = githubRequest(ctx, client, token, http.MethodPost, endpoint, requestBody)
	if err != nil {
		return nil, fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to create repository"); err != nil {
		return nil, err
	}

	var repo templateRepoResponse
	if err := json.Unmarshal(body, &repo); err != nil {
		return nil, errors.New("Failed to parse repository response")
	}
	return &repo, nil
}

func createGithubInitialCommit(
	ctx context.Context,
	client *http.Client,
	token, owner, repo, branch string,
	files []templateFileContent,
	payload templateRepoRequest,
) error {
	if len(files) == 0 {
		return errors.New("Template repository is empty")
	}

	treeEntries := make([]githubCreateTreeEntry, 0, len(files))
	for _, file := range files {
		blobSha, err := createGithubBlob(ctx, client, token, owner, repo, file.ContentBase64)
		if err != nil {
			return err
		}
		mode := strings.TrimSpace(file.Mode)
		if mode == "" {
			mode = "100644"
		}
		treeEntries = append(treeEntries, githubCreateTreeEntry{
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

	message := fmt.Sprintf(
		"Initial commit from template %s/%s",
		payload.TemplateOwner,
		payload.TemplateRepo,
	)
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
	body, status, err := githubRequest(ctx, client, token, http.MethodPost, blobURL, requestBody)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to create template blob"); err != nil {
		return "", err
	}

	var blob githubCreateBlobResponse
	if err := json.Unmarshal(body, &blob); err != nil {
		return "", errors.New("Failed to parse created blob")
	}
	if strings.TrimSpace(blob.Sha) == "" {
		return "", errors.New("Created blob SHA missing")
	}
	return blob.Sha, nil
}

func createGithubTree(
	ctx context.Context,
	client *http.Client,
	token, owner, repo string,
	entries []githubCreateTreeEntry,
) (string, error) {
	treeURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/git/trees", owner, repo)
	requestBody := map[string]interface{}{
		"tree": entries,
	}
	body, status, err := githubRequest(ctx, client, token, http.MethodPost, treeURL, requestBody)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to create template tree"); err != nil {
		return "", err
	}

	var tree githubCreateTreeResponse
	if err := json.Unmarshal(body, &tree); err != nil {
		return "", errors.New("Failed to parse created tree")
	}
	if strings.TrimSpace(tree.Sha) == "" {
		return "", errors.New("Created tree SHA missing")
	}
	return tree.Sha, nil
}

func createGithubCommit(
	ctx context.Context,
	client *http.Client,
	token, owner, repo, message, treeSha string,
) (string, error) {
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
	body, status, err := githubRequest(ctx, client, token, http.MethodPost, commitURL, requestBody)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to create initial commit"); err != nil {
		return "", err
	}

	var commit githubCreateCommitResponse
	if err := json.Unmarshal(body, &commit); err != nil {
		return "", errors.New("Failed to parse created commit")
	}
	if strings.TrimSpace(commit.Sha) == "" {
		return "", errors.New("Created commit SHA missing")
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
	body, status, err := githubRequest(ctx, client, token, http.MethodPatch, updateURL, updateBody)
	if err != nil {
		return fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := githubResponseError(status, body, "Failed to update repository branch"); err != nil {
		return err
	}
	return nil
}

func deleteGithubRepo(ctx context.Context, client *http.Client, token, owner, repo string) error {
	endpoint := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, repo)
	body, status, err := githubRequest(ctx, client, token, http.MethodDelete, endpoint, nil)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound || status == http.StatusNoContent || (status >= 200 && status < 300) {
		return nil
	}
	return githubResponseError(status, body, "Failed to clean up repository after template copy error")
}

func githubRequest(ctx context.Context, client *http.Client, token, method, url string, payload interface{}) ([]byte, int, error) {
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

type commitEntry struct {
	Sha     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

type githubCommitListEntry struct {
	Sha    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Name string `json:"name"`
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

func ListCommits(c *gin.Context) {
	owner := strings.TrimSpace(c.Query("owner"))
	repo := strings.TrimSpace(c.Query("repo"))
	branch := strings.TrimSpace(c.Query("branch"))
	projectID := strings.TrimSpace(c.Query("projectId"))
	scmCredentialID := strings.TrimSpace(c.Query("scmCredentialId"))

	if owner == "" || repo == "" {
		shared.RespondError(c, http.StatusBadRequest, "owner and repo are required")
		return
	}
	if branch == "" {
		branch = "main"
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	scmCred, err := resolveScmCredential(ctx, scmCredentialID, projectID)
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "SCM credential not found")
		return
	}

	token := strings.TrimSpace(shared.StringValue(scmCred["token"]))
	if token == "" {
		shared.RespondError(c, http.StatusBadRequest, "SCM credential missing token")
		return
	}

	client := &http.Client{Timeout: 15 * time.Second}
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/%s/commits?sha=%s&per_page=20",
		url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(branch))

	body, status, err := githubRequest(ctx, client, token, http.MethodGet, apiURL, nil)
	if err != nil {
		shared.RespondError(c, http.StatusBadGateway, "Failed to fetch commits from GitHub")
		return
	}
	if err := githubResponseError(status, body, "Failed to fetch commits"); err != nil {
		shared.RespondError(c, http.StatusBadGateway, err.Error())
		return
	}

	var ghCommits []githubCommitListEntry
	if err := json.Unmarshal(body, &ghCommits); err != nil {
		shared.RespondError(c, http.StatusBadGateway, "Failed to parse commits response")
		return
	}

	commits := make([]commitEntry, 0, len(ghCommits))
	for _, gh := range ghCommits {
		message := gh.Commit.Message
		if idx := strings.Index(message, "\n"); idx > 0 {
			message = message[:idx]
		}
		commits = append(commits, commitEntry{
			Sha:     gh.Sha,
			Message: message,
			Author:  gh.Commit.Author.Name,
			Date:    gh.Commit.Author.Date,
		})
	}

	c.JSON(http.StatusOK, commits)
}

func buildReleaseaManagedMarker(payload templateRepoRequest) (string, error) {
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

func decodeGithubBlobContent(blob *githubBlobInfo) ([]byte, error) {
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
