package models

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
