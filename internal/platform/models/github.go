package models

// GitHubCommitHeadResponse is used by deploy resolution when reading the latest
// commit SHA from GitHub.
type GitHubCommitHeadResponse struct {
	Sha string `json:"sha"`
}
