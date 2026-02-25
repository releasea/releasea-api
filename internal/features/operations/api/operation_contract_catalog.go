package operations

// OperationContractSpec defines the stable contract for operations published by the API
// and consumed by the Worker through the queue.
type OperationContractSpec struct {
	Type              string
	Version           string
	ResourceType      string
	PayloadRequired   []string
	PayloadOptional   []string
	StatusLifecycle   []string
	RetryPolicy       string
	RollbackPolicy    string
	CompatibilityRule string
	HandlerAliasOf    string
}

var operationTypeCatalogOrder = []string{
	OperationTypeServiceDeploy,
	OperationTypeServicePromoteCanary,
	OperationTypeServiceDelete,
	OperationTypeRuleDeploy,
	OperationTypeRulePublish,
	OperationTypeRuleDelete,
	OperationTypeWorkerRestart,
}

var operationStatusLifecycle = []string{
	StatusQueued,
	StatusInProgress,
	StatusSucceeded,
	StatusFailed,
}

var operationContractCatalog = map[string]OperationContractSpec{
	OperationTypeServiceDeploy: {
		Type:              OperationTypeServiceDeploy,
		Version:           OperationContractCatalogVersion,
		ResourceType:      "service",
		PayloadRequired:   []string{"environment", "strategyType"},
		PayloadOptional:   []string{"version", "commitSha", "image", "resources", "resourcesYaml", "trigger"},
		StatusLifecycle:   operationStatusLifecycle,
		RetryPolicy:       "worker transient retry with env-configured attempts/delay",
		RollbackPolicy:    "strategy-aware rollback/failure finalization",
		CompatibilityRule: "additive payload changes only; keep required fields stable",
	},
	OperationTypeServicePromoteCanary: {
		Type:              OperationTypeServicePromoteCanary,
		Version:           OperationContractCatalogVersion,
		ResourceType:      "service",
		PayloadRequired:   []string{"environment"},
		PayloadOptional:   []string{},
		StatusLifecycle:   operationStatusLifecycle,
		RetryPolicy:       "no automatic retries",
		RollbackPolicy:    "terminal failure, no automated rollback orchestration",
		CompatibilityRule: "additive payload changes only",
	},
	OperationTypeServiceDelete: {
		Type:              OperationTypeServiceDelete,
		Version:           OperationContractCatalogVersion,
		ResourceType:      "service",
		PayloadRequired:   []string{"environment"},
		PayloadOptional:   []string{},
		StatusLifecycle:   operationStatusLifecycle,
		RetryPolicy:       "no automatic retries",
		RollbackPolicy:    "terminal failure, resource cleanup remains explicit",
		CompatibilityRule: "additive payload changes only",
	},
	OperationTypeRuleDeploy: {
		Type:              OperationTypeRuleDeploy,
		Version:           OperationContractCatalogVersion,
		ResourceType:      "rule",
		PayloadRequired:   []string{"environment", "internal", "external", "prevGateways", "nextGateways", "prevStatus", "prevLastPublishedAt"},
		PayloadOptional:   []string{"canaryPercentOverride"},
		StatusLifecycle:   operationStatusLifecycle,
		RetryPolicy:       "worker transient retry with env-configured attempts/delay",
		RollbackPolicy:    "API reverts previous gateways/status on failure",
		CompatibilityRule: "additive payload changes only; preserve rollback fields",
	},
	OperationTypeRulePublish: {
		Type:              OperationTypeRulePublish,
		Version:           OperationContractCatalogVersion,
		ResourceType:      "rule",
		PayloadRequired:   []string{"environment", "internal", "external", "prevGateways", "nextGateways", "prevStatus", "prevLastPublishedAt"},
		PayloadOptional:   []string{"canaryPercentOverride"},
		StatusLifecycle:   operationStatusLifecycle,
		RetryPolicy:       "worker transient retry with env-configured attempts/delay",
		RollbackPolicy:    "API reverts previous gateways/status on failure",
		CompatibilityRule: "additive payload changes only; preserve rollback fields",
		HandlerAliasOf:    OperationTypeRuleDeploy,
	},
	OperationTypeRuleDelete: {
		Type:              OperationTypeRuleDelete,
		Version:           OperationContractCatalogVersion,
		ResourceType:      "rule",
		PayloadRequired:   []string{"environment", "serviceId", "serviceName", "ruleName", "action"},
		PayloadOptional:   []string{},
		StatusLifecycle:   operationStatusLifecycle,
		RetryPolicy:       "no automatic retries",
		RollbackPolicy:    "terminal failure, no metadata rollback required",
		CompatibilityRule: "additive payload changes only",
	},
	OperationTypeWorkerRestart: {
		Type:              OperationTypeWorkerRestart,
		Version:           OperationContractCatalogVersion,
		ResourceType:      "worker",
		PayloadRequired:   []string{"deploymentName", "deploymentNamespace"},
		PayloadOptional:   []string{},
		StatusLifecycle:   operationStatusLifecycle,
		RetryPolicy:       "no automatic retries",
		RollbackPolicy:    "terminal failure, manual retry supported",
		CompatibilityRule: "additive payload changes only",
	},
}

func SupportedOperationTypes() []string {
	return append([]string(nil), operationTypeCatalogOrder...)
}

func SupportedOperationStatuses() []string {
	return append([]string(nil), operationStatusLifecycle...)
}

func OperationContractCatalog() map[string]OperationContractSpec {
	cloned := make(map[string]OperationContractSpec, len(operationContractCatalog))
	for operationType, spec := range operationContractCatalog {
		cloned[operationType] = cloneOperationContractSpec(spec)
	}
	return cloned
}

func OperationContractByType(operationType string) (OperationContractSpec, bool) {
	spec, ok := operationContractCatalog[operationType]
	if !ok {
		return OperationContractSpec{}, false
	}
	return cloneOperationContractSpec(spec), true
}

func IsSupportedOperationType(operationType string) bool {
	_, ok := operationContractCatalog[operationType]
	return ok
}

func cloneOperationContractSpec(spec OperationContractSpec) OperationContractSpec {
	spec.PayloadRequired = append([]string(nil), spec.PayloadRequired...)
	spec.PayloadOptional = append([]string(nil), spec.PayloadOptional...)
	spec.StatusLifecycle = append([]string(nil), spec.StatusLifecycle...)
	return spec
}
