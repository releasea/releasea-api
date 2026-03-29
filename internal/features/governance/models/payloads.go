package models

type DeployApprovalSettings struct {
	Enabled      bool     `json:"enabled"`
	Environments []string `json:"environments"`
	MinApprovers int      `json:"minApprovers"`
}

type DeployPolicyRule struct {
	Environment              string   `json:"environment"`
	AllowAutoDeploy          bool     `json:"allowAutoDeploy"`
	RequireExplicitVersion   bool     `json:"requireExplicitVersion"`
	BlockExternalExposure    bool     `json:"blockExternalExposure"`
	AllowedProfileIDs        []string `json:"allowedProfileIds"`
	AllowedSCMProviders      []string `json:"allowedScmProviders"`
	AllowedRegistryProviders []string `json:"allowedRegistryProviders"`
	AllowedSecretProviders   []string `json:"allowedSecretProviders"`
	AllowedSourceTypes       []string `json:"allowedSourceTypes"`
	AllowedRegistries        []string `json:"allowedRegistries"`
	AllowedStrategies        []string `json:"allowedStrategies"`
	MaxReplicas              int      `json:"maxReplicas"`
}

type DeployPolicySettings struct {
	Enabled bool               `json:"enabled"`
	DryRun  bool               `json:"dryRun"`
	Rules   []DeployPolicyRule `json:"rules"`
}

type RulePublishApprovalSettings struct {
	Enabled      bool `json:"enabled"`
	ExternalOnly bool `json:"externalOnly"`
	MinApprovers int  `json:"minApprovers"`
}

type GovernanceSettingsPayload struct {
	DeployApproval      DeployApprovalSettings      `json:"deployApproval"`
	DeployPolicy        DeployPolicySettings        `json:"deployPolicy"`
	RulePublishApproval RulePublishApprovalSettings `json:"rulePublishApproval"`
	AuditRetentionDays  int                         `json:"auditRetentionDays"`
}

type CreateTemporaryExceptionPayload struct {
	Policy      string   `json:"policy"`
	ServiceID   string   `json:"serviceId"`
	Environment string   `json:"environment"`
	Codes       []string `json:"codes"`
	Reason      string   `json:"reason"`
	ExpiresAt   string   `json:"expiresAt"`
}

type CreateApprovalPayload struct {
	Type         string                 `json:"type"`
	ResourceID   string                 `json:"resourceId"`
	ResourceName string                 `json:"resourceName"`
	Environment  string                 `json:"environment"`
	Metadata     map[string]interface{} `json:"metadata"`
}

type ReviewApprovalPayload struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}
