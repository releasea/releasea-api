package models

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
