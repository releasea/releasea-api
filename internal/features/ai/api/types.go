package ai

import "time"

const (
	providerTypeOpenAI           = "openai"
	providerTypeOpenAICompatible = "openai-compatible"
	analysisFailedDeploy         = "failed-deploy"
	analysisHealthSummary        = "health-summary"
	analysisCorrectionPlan       = "correction-plan"
)

type providerPayload struct {
	Name                string   `json:"name"`
	Type                string   `json:"type"`
	BaseURL             string   `json:"baseUrl"`
	APIKey              string   `json:"apiKey"`
	Model               string   `json:"model"`
	Enabled             *bool    `json:"enabled"`
	Default             bool     `json:"default"`
	AllowPrivateNetwork bool     `json:"allowPrivateNetwork"`
	ExternalEgress      *bool    `json:"externalEgress"`
	TimeoutSeconds      int      `json:"timeoutSeconds"`
	MaxInputChars       int      `json:"maxInputChars"`
	MaxOutputTokens     int      `json:"maxOutputTokens"`
	DailyTokenLimit     int64    `json:"dailyTokenLimit"`
	RetentionDays       int      `json:"retentionDays"`
	Capabilities        []string `json:"capabilities"`
}

type providerConfig struct {
	ID                  string
	Name                string
	Type                string
	BaseURL             string
	APIKey              string
	Model               string
	Enabled             bool
	Default             bool
	AllowPrivateNetwork bool
	ExternalEgress      bool
	Timeout             time.Duration
	MaxInputChars       int
	MaxOutputTokens     int
	DailyTokenLimit     int64
	RetentionDays       int
	Capabilities        []string
}

type analysisPayload struct {
	Kind        string `json:"kind"`
	ProviderID  string `json:"providerId"`
	Question    string `json:"question"`
	Environment string `json:"environment"`
}

type analysisResult struct {
	Summary         string            `json:"summary"`
	Severity        string            `json:"severity"`
	Findings        []analysisFinding `json:"findings"`
	Recommendations []recommendation  `json:"recommendations"`
	Limitations     []string          `json:"limitations"`
}

type analysisFinding struct {
	Title       string   `json:"title"`
	Explanation string   `json:"explanation"`
	EvidenceIDs []string `json:"evidenceIds"`
}

type recommendation struct {
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Risk        string   `json:"risk"`
	EvidenceIDs []string `json:"evidenceIds"`
}

type usage struct {
	InputTokens  int64 `json:"inputTokens"`
	OutputTokens int64 `json:"outputTokens"`
	TotalTokens  int64 `json:"totalTokens"`
}

type inferenceResult struct {
	ResponseID string
	Model      string
	Text       string
	Usage      usage
}
