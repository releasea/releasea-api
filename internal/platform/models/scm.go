package models

type CommitEntry struct {
	Sha     string `json:"sha"`
	Message string `json:"message"`
	Author  string `json:"author"`
	Date    string `json:"date"`
}

type GitHubCommitAuthor struct {
	Name string `json:"name"`
	Date string `json:"date"`
}

type GitHubCommitBody struct {
	Message string             `json:"message"`
	Author  GitHubCommitAuthor `json:"author"`
}

type GitHubCommitListEntry struct {
	Sha    string           `json:"sha"`
	Commit GitHubCommitBody `json:"commit"`
}

type GitHubRepoInfo struct {
	DefaultBranch string `json:"default_branch"`
}

type GitHubUserInfo struct {
	Login string `json:"login"`
}

type GitHubRefObject struct {
	Sha string `json:"sha"`
}

type GitHubRefInfo struct {
	Object GitHubRefObject `json:"object"`
}

type GitHubCommitTree struct {
	Sha string `json:"sha"`
}

type GitHubCommitInfo struct {
	Tree GitHubCommitTree `json:"tree"`
}

type GitHubTreeEntry struct {
	Path string `json:"path"`
	Sha  string `json:"sha"`
	Type string `json:"type"`
	Mode string `json:"mode"`
}

type GitHubTreeInfo struct {
	Tree      []GitHubTreeEntry `json:"tree"`
	Truncated bool              `json:"truncated"`
}

type GitHubBlobInfo struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

type GitHubCreateBlobResponse struct {
	Sha string `json:"sha"`
}

type GitHubCreateTreeResponse struct {
	Sha string `json:"sha"`
}

type GitHubCreateCommitResponse struct {
	Sha string `json:"sha"`
}

type GitHubCreateTreeEntry struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	Type string `json:"type"`
	Sha  string `json:"sha"`
}

type DesiredStatePullRequestRequest struct {
	RepoURL         string                        `json:"repoUrl"`
	BaseBranch      string                        `json:"baseBranch"`
	BranchName      string                        `json:"branchName"`
	FilePath        string                        `json:"filePath"`
	Content         string                        `json:"content"`
	CommitMessage   string                        `json:"commitMessage"`
	Title           string                        `json:"title"`
	Body            string                        `json:"body"`
	AdditionalFiles []DesiredStatePullRequestFile `json:"additionalFiles,omitempty"`
}

type DesiredStatePullRequestResponse struct {
	URL        string   `json:"url"`
	Number     int      `json:"number"`
	BaseBranch string   `json:"baseBranch"`
	BranchName string   `json:"branchName"`
	FilePath   string   `json:"filePath"`
	FilePaths  []string `json:"filePaths,omitempty"`
	Title      string   `json:"title"`
}

type DesiredStatePullRequestFile struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type GitHubRefCreateResponse struct {
	Ref string `json:"ref"`
}

type GitHubContentItemResponse struct {
	Sha string `json:"sha"`
}

type GitHubPullRequestResponse struct {
	HTMLURL string `json:"html_url"`
	Number  int    `json:"number"`
}

type TemplateFileContent struct {
	Path          string
	Mode          string
	ContentBase64 string
}

type TemplateManifestSource struct {
	Owner string `yaml:"owner"`
	Repo  string `yaml:"repo"`
	Path  string `yaml:"path"`
}

type TemplateManifest struct {
	APIVersion   string                 `yaml:"apiVersion"`
	Kind         string                 `yaml:"kind"`
	ID           string                 `yaml:"id"`
	Name         string                 `yaml:"name"`
	TemplateType string                 `yaml:"templateType"`
	Version      string                 `yaml:"version"`
	Source       TemplateManifestSource `yaml:"source"`
}

type TemplateRepoRequest struct {
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

type GitHubRepoOwner struct {
	Login string `json:"login"`
}

type TemplateRepoResponse struct {
	ID            int             `json:"id"`
	FullName      string          `json:"full_name"`
	HTMLURL       string          `json:"html_url"`
	CloneURL      string          `json:"clone_url"`
	SSHURL        string          `json:"ssh_url"`
	DefaultBranch string          `json:"default_branch"`
	Private       bool            `json:"private"`
	Owner         GitHubRepoOwner `json:"owner"`
}
