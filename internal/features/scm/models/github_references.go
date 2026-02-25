package models

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
