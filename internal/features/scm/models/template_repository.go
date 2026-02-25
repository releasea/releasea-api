package models

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
