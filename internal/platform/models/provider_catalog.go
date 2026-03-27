package models

// ProviderCatalog describes the built-in provider surface currently supported
// by the platform. The catalog is intentionally static in this first phase so
// the Console can stop hardcoding provider options while the runtime keeps its
// existing credential and settings flows.
type ProviderCatalog struct {
	Version       string           `json:"version"`
	SCM           ProviderCategory `json:"scm"`
	Registry      ProviderCategory `json:"registry"`
	Secrets       ProviderCategory `json:"secrets"`
	Identity      ProviderCategory `json:"identity"`
	Notifications ProviderCategory `json:"notifications"`
}

type ProviderStatusCatalog struct {
	Version       string                 `json:"version"`
	SCM           ProviderStatusCategory `json:"scm"`
	Registry      ProviderStatusCategory `json:"registry"`
	Secrets       ProviderStatusCategory `json:"secrets"`
	Identity      ProviderStatusCategory `json:"identity"`
	Notifications ProviderStatusCategory `json:"notifications"`
}

type ProviderHealthCatalog struct {
	Version       string                 `json:"version"`
	SCM           ProviderHealthCategory `json:"scm"`
	Registry      ProviderHealthCategory `json:"registry"`
	Secrets       ProviderHealthCategory `json:"secrets"`
	Identity      ProviderHealthCategory `json:"identity"`
	Notifications ProviderHealthCategory `json:"notifications"`
}

type ProviderCategory struct {
	Kind            string               `json:"kind"`
	Label           string               `json:"label"`
	Description     string               `json:"description,omitempty"`
	DefaultProvider string               `json:"defaultProvider,omitempty"`
	Providers       []ProviderDefinition `json:"providers"`
}

type ProviderDefinition struct {
	ID             string   `json:"id"`
	Label          string   `json:"label"`
	Description    string   `json:"description,omitempty"`
	AuthModes      []string `json:"authModes,omitempty"`
	Capabilities   []string `json:"capabilities,omitempty"`
	ScopeSupport   []string `json:"scopeSupport,omitempty"`
	ConfigFields   []string `json:"configFields,omitempty"`
	Implementation string   `json:"implementation,omitempty"`
}

type ProviderStatusCategory struct {
	Kind      string           `json:"kind"`
	Providers []ProviderStatus `json:"providers"`
}

type ProviderHealthCategory struct {
	Kind        string                `json:"kind"`
	Healthy     int                   `json:"healthy"`
	Unhealthy   int                   `json:"unhealthy"`
	Unsupported int                   `json:"unsupported,omitempty"`
	Disabled    int                   `json:"disabled,omitempty"`
	Checks      []ProviderHealthCheck `json:"checks"`
}

type ProviderStatus struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	State          string `json:"state"`
	Message        string `json:"message,omitempty"`
	Configured     bool   `json:"configured"`
	Default        bool   `json:"default,omitempty"`
	ResourceCount  int    `json:"resourceCount,omitempty"`
	Implementation string `json:"implementation,omitempty"`
}

type ProviderHealthCheck struct {
	ProviderID     string `json:"providerId"`
	ProviderLabel  string `json:"providerLabel"`
	ResourceID     string `json:"resourceId,omitempty"`
	ResourceLabel  string `json:"resourceLabel,omitempty"`
	Scope          string `json:"scope,omitempty"`
	State          string `json:"state"`
	Message        string `json:"message,omitempty"`
	Default        bool   `json:"default,omitempty"`
	Implementation string `json:"implementation,omitempty"`
}
