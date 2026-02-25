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
