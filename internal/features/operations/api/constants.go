package operations

const (
	OperationContractCatalogVersion = "v1"

	StatusQueued     = "queued"
	StatusInProgress = "in-progress"
	StatusSucceeded  = "succeeded"
	StatusFailed     = "failed"

	OperationTypeServiceDeploy        = "service.deploy"
	OperationTypeServicePromoteCanary = "service.promote-canary"
	OperationTypeServiceDelete        = "service.delete"
	OperationTypeRuleDeploy           = "rule.deploy"
	OperationTypeRulePublish          = "rule.publish"
	OperationTypeRuleDelete           = "rule.delete"
	OperationTypeWorkerRestart        = "worker.restart"
)
