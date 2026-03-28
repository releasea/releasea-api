package models

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
