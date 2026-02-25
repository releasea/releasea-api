package models

type DeployApprovalSettings struct {
	Enabled      bool     `json:"enabled"`
	Environments []string `json:"environments"`
	MinApprovers int      `json:"minApprovers"`
}

type RulePublishApprovalSettings struct {
	Enabled      bool `json:"enabled"`
	ExternalOnly bool `json:"externalOnly"`
	MinApprovers int  `json:"minApprovers"`
}

type GovernanceSettingsPayload struct {
	DeployApproval      DeployApprovalSettings      `json:"deployApproval"`
	RulePublishApproval RulePublishApprovalSettings `json:"rulePublishApproval"`
	AuditRetentionDays  int                         `json:"auditRetentionDays"`
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
