package governance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"releaseaapi/internal/features/operations/api"
	operationqueue "releaseaapi/internal/platform/queue"
	"releaseaapi/internal/platform/shared"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func extractApprovalReviews(raw interface{}) []bson.M {
	switch value := raw.(type) {
	case []bson.M:
		return append([]bson.M{}, value...)
	case []interface{}:
		reviews := make([]bson.M, 0, len(value))
		for _, item := range value {
			switch review := item.(type) {
			case bson.M:
				reviews = append(reviews, review)
			case map[string]interface{}:
				reviews = append(reviews, bson.M(review))
			}
		}
		return reviews
	default:
		return []bson.M{}
	}
}

func hasReviewerReview(reviews []bson.M, reviewerID string) bool {
	reviewerID = strings.TrimSpace(reviewerID)
	if reviewerID == "" {
		return false
	}
	for _, review := range reviews {
		reviewedBy := shared.MapPayload(review["reviewedBy"])
		if strings.EqualFold(strings.TrimSpace(shared.StringValue(reviewedBy["id"])), reviewerID) {
			return true
		}
	}
	return false
}

func countReviewsByStatus(reviews []bson.M, status string) int {
	total := 0
	for _, review := range reviews {
		if shared.NormalizeGovernanceApprovalStatus(shared.StringValue(review["status"])) == status {
			total++
		}
	}
	return total
}

func resolveRequiredApprovers(ctx context.Context, approval bson.M) int {
	requiredApprovers := shared.IntValue(approval["requiredApprovers"])
	if requiredApprovers > 0 {
		return requiredApprovers
	}
	settings, err := shared.LoadGovernanceSettings(ctx)
	if err != nil {
		return 1
	}
	return shared.MinApproversForApprovalType(settings, shared.StringValue(approval["type"]))
}

func mergeApprovalWithReviewState(
	approval bson.M,
	reviews []bson.M,
	approvedCount int,
	rejectedCount int,
	requiredApprovers int,
) bson.M {
	merged := bson.M{}
	for key, value := range approval {
		merged[key] = value
	}
	merged["reviews"] = reviews
	merged["approvalsCount"] = approvedCount
	merged["rejectionsCount"] = rejectedCount
	merged["requiredApprovers"] = requiredApprovers
	return merged
}

func executeApprovedAction(ctx context.Context, approval bson.M) (bson.M, error) {
	switch shared.NormalizeGovernanceApprovalType(shared.StringValue(approval["type"])) {
	case shared.GovernanceApprovalTypeDeploy:
		return executeApprovedDeploy(ctx, approval)
	case shared.GovernanceApprovalTypeRulePublish:
		return executeApprovedRulePublish(ctx, approval)
	default:
		return nil, fmt.Errorf("unsupported approval type")
	}
}

func executeApprovedDeploy(ctx context.Context, approval bson.M) (bson.M, error) {
	metadata := shared.MapPayload(approval["metadata"])
	action := shared.MapPayload(metadata["action"])
	if kind := strings.TrimSpace(shared.StringValue(action["kind"])); kind != "" && kind != operations.OperationTypeServiceDeploy {
		return nil, fmt.Errorf("unsupported deploy action %q", kind)
	}

	serviceID := strings.TrimSpace(shared.StringValue(action["serviceId"]))
	if serviceID == "" {
		serviceID = strings.TrimSpace(shared.StringValue(approval["resourceId"]))
	}
	if serviceID == "" {
		return nil, fmt.Errorf("missing service id")
	}

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		return nil, fmt.Errorf("service not found")
	}

	environment := shared.NormalizeOperationEnvironment(shared.StringValue(action["environment"]))
	if environment == "" {
		environment = shared.NormalizeOperationEnvironment(shared.StringValue(approval["environment"]))
	}

	activeWorker, workerErr := shared.HasActiveWorkerForEnvironment(ctx, environment)
	if workerErr != nil {
		return nil, workerErr
	}
	if !activeWorker {
		return nil, errors.New(shared.WorkerUnavailableMessage(environment))
	}

	blockedDeploy, err := shared.FindOne(ctx, shared.Collection(shared.DeploysCollection), bson.M{
		"serviceId":   serviceID,
		"environment": environment,
		"status": bson.M{
			"$in": operations.DeployQueueBlockingStatuses(),
		},
	})
	if err == nil {
		return nil, fmt.Errorf("active deploy %s is already in progress", shared.StringValue(blockedDeploy["id"]))
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	deployVersion := strings.TrimSpace(shared.StringValue(action["version"]))
	if deployVersion == "" {
		deployVersion = strings.TrimSpace(shared.StringValue(metadata["version"]))
	}
	deployBranch := strings.TrimSpace(shared.StringValue(action["branch"]))
	if deployBranch == "" {
		deployBranch = strings.TrimSpace(shared.StringValue(metadata["branch"]))
	}
	if deployBranch == "" {
		deployBranch = "main"
	}
	trigger := strings.ToLower(strings.TrimSpace(shared.StringValue(action["trigger"])))
	if trigger == "" {
		trigger = "manual"
	}
	requestedBy := strings.TrimSpace(shared.StringValue(action["requestedBy"]))
	if requestedBy == "" {
		requestedBy = approvalRequesterName(approval)
	}
	strategyType := strings.ToLower(strings.TrimSpace(shared.StringValue(action["strategyType"])))
	if strategyType == "" {
		strategyType = resolveDeployStrategyType(service)
	}
	deployImage := strings.TrimSpace(shared.StringValue(action["image"]))
	resources := extractActionResources(action["resources"])
	resourcesYAML := strings.TrimSpace(shared.StringValue(action["resourcesYaml"]))

	now := shared.NowISO()
	deployID := "deploy-" + uuid.NewString()
	deployDoc := bson.M{
		"_id":            deployID,
		"id":             deployID,
		"serviceId":      serviceID,
		"status":         operations.DeployStatusRequested,
		"environment":    environment,
		"commit":         deployVersion,
		"branch":         deployBranch,
		"triggeredBy":    requestedBy,
		"trigger":        trigger,
		"startedAt":      now,
		"logs":           []interface{}{},
		"strategyStatus": buildApprovalDeployStrategyStatus(service, operations.DeployStatusRequested, "Deployment requested", now),
	}
	if deployImage != "" {
		deployDoc["image"] = deployImage
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.DeploysCollection), deployDoc); err != nil {
		return nil, err
	}

	operationID := "op-" + uuid.NewString()
	opPayload := bson.M{
		"environment":  environment,
		"version":      deployVersion,
		"commitSha":    deployVersion,
		"strategyType": strategyType,
		"trigger":      trigger,
		"resources":    resources,
	}
	if deployImage != "" {
		opPayload["image"] = deployImage
	}
	if resourcesYAML != "" {
		opPayload["resourcesYaml"] = resourcesYAML
	}
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         operations.OperationTypeServiceDeploy,
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload":      opPayload,
		"deployId":     deployID,
		"requestedBy":  requestedBy,
		"serviceName":  shared.StringValue(service["name"]),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return nil, err
	}

	execution := bson.M{
		"status":      "queued",
		"operationId": operationID,
		"deployId":    deployID,
		"updatedAt":   now,
	}
	if err := operationqueue.PublishOperation(ctx, operationID); err != nil {
		operationqueue.RecordOperationDispatchError(ctx, operationID, err)
		execution["warning"] = "Worker queue unavailable. Operation remains queued."
	}
	return execution, nil
}

func executeApprovedRulePublish(ctx context.Context, approval bson.M) (bson.M, error) {
	metadata := shared.MapPayload(approval["metadata"])
	action := shared.MapPayload(metadata["action"])
	if kind := strings.TrimSpace(shared.StringValue(action["kind"])); kind != "" && kind != operations.OperationTypeRulePublish {
		return nil, fmt.Errorf("unsupported rule action %q", kind)
	}

	ruleID := strings.TrimSpace(shared.StringValue(action["ruleId"]))
	if ruleID == "" {
		ruleID = strings.TrimSpace(shared.StringValue(approval["resourceId"]))
	}
	if ruleID == "" {
		return nil, fmt.Errorf("missing rule id")
	}

	rule, err := shared.FindOne(ctx, shared.Collection(shared.RulesCollection), bson.M{"id": ruleID})
	if err != nil {
		return nil, fmt.Errorf("rule not found")
	}
	environment := shared.NormalizeOperationEnvironment(shared.StringValue(action["environment"]))
	if environment == "" {
		environment = shared.NormalizeOperationEnvironment(shared.StringValue(approval["environment"]))
	}
	if environment == "" {
		environment = shared.NormalizeOperationEnvironment(shared.StringValue(rule["environment"]))
	}

	activeWorker, workerErr := shared.HasActiveWorkerForEnvironment(ctx, environment)
	if workerErr != nil {
		return nil, workerErr
	}
	if !activeWorker {
		return nil, errors.New(shared.WorkerUnavailableMessage(environment))
	}

	activeOperation, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{
		"type":       bson.M{"$in": []string{operations.OperationTypeRuleDeploy, operations.OperationTypeRulePublish}},
		"resourceId": ruleID,
		"status": bson.M{
			"$in": []string{operations.StatusQueued, operations.StatusInProgress},
		},
	})
	if err == nil {
		return nil, fmt.Errorf("active rule operation %s is already in progress", shared.StringValue(activeOperation["id"]))
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	internal := shared.BoolValue(action["internal"])
	external := shared.BoolValue(action["external"])
	prevGateways := shared.ToStringSlice(rule["gateways"])
	nextGateways := operations.BuildGateways(prevGateways, internal, external, environment)

	now := shared.NowISO()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, bson.M{
		"gateways":    nextGateways,
		"status":      operations.StatusQueued,
		"environment": environment,
		"updatedAt":   now,
	}); err != nil {
		return nil, err
	}

	serviceID := strings.TrimSpace(shared.StringValue(action["serviceId"]))
	if serviceID == "" {
		serviceID = shared.StringValue(rule["serviceId"])
	}
	requestedBy := strings.TrimSpace(shared.StringValue(action["requestedBy"]))
	if requestedBy == "" {
		requestedBy = approvalRequesterName(approval)
	}

	ruleDeployID := "rdeploy-" + uuid.NewString()
	ruleDeployDoc := bson.M{
		"_id":         ruleDeployID,
		"id":          ruleDeployID,
		"ruleId":      ruleID,
		"serviceId":   serviceID,
		"status":      operations.StatusQueued,
		"environment": environment,
		"triggeredBy": requestedBy,
		"startedAt":   now,
		"logs":        []interface{}{},
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.RuleDeploysCollection), ruleDeployDoc); err != nil {
		return nil, err
	}

	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         operations.OperationTypeRuleDeploy,
		"resourceType": "rule",
		"resourceId":   ruleID,
		"ruleDeployId": ruleDeployID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"internal":            internal,
			"external":            external,
			"environment":         environment,
			"prevGateways":        prevGateways,
			"nextGateways":        nextGateways,
			"prevStatus":          shared.StringValue(rule["status"]),
			"prevLastPublishedAt": shared.StringValue(rule["lastPublishedAt"]),
		},
		"requestedBy": requestedBy,
		"serviceName": shared.StringValue(rule["serviceName"]),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return nil, err
	}

	execution := bson.M{
		"status":       "queued",
		"operationId":  operationID,
		"ruleDeployId": ruleDeployID,
		"updatedAt":    now,
	}
	if err := operationqueue.PublishOperation(ctx, operationID); err != nil {
		operationqueue.RecordOperationDispatchError(ctx, operationID, err)
		execution["warning"] = "Worker queue unavailable. Operation remains queued."
	}
	return execution, nil
}

func extractActionResources(raw interface{}) []map[string]interface{} {
	switch value := raw.(type) {
	case []map[string]interface{}:
		return append([]map[string]interface{}{}, value...)
	case []bson.M:
		items := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			items = append(items, map[string]interface{}(item))
		}
		return items
	case []interface{}:
		items := make([]map[string]interface{}, 0, len(value))
		for _, item := range value {
			switch resource := item.(type) {
			case map[string]interface{}:
				items = append(items, resource)
			case bson.M:
				items = append(items, map[string]interface{}(resource))
			}
		}
		return items
	default:
		return []map[string]interface{}{}
	}
}

func approvalRequesterName(approval bson.M) string {
	requestedBy := shared.MapPayload(approval["requestedBy"])
	name := strings.TrimSpace(shared.StringValue(requestedBy["name"]))
	if name == "" {
		name = "System"
	}
	return name
}

func resolveDeployStrategyType(service bson.M) string {
	strategy := shared.MapPayload(service["deploymentStrategy"])
	strategyType := strings.ToLower(strings.TrimSpace(shared.StringValue(strategy["type"])))
	switch strategyType {
	case "canary", "blue-green", "rolling":
		return strategyType
	default:
		return "rolling"
	}
}

func buildApprovalDeployStrategyStatus(service bson.M, phase, summary, now string) bson.M {
	strategy := shared.MapPayload(service["deploymentStrategy"])
	strategyType := resolveDeployStrategyType(service)

	details := bson.M{}
	switch strategyType {
	case "canary":
		canaryPercent := shared.IntValue(strategy["canaryPercent"])
		if canaryPercent <= 0 {
			canaryPercent = 10
		}
		if canaryPercent > 50 {
			canaryPercent = 50
		}
		details["exposurePercent"] = canaryPercent
		details["stablePercent"] = 100 - canaryPercent
	case "blue-green":
		primary := strings.ToLower(strings.TrimSpace(shared.StringValue(strategy["blueGreenPrimary"])))
		if primary != "green" {
			primary = "blue"
		}
		details["activeSlot"] = primary
		if primary == "blue" {
			details["inactiveSlot"] = "green"
		} else {
			details["inactiveSlot"] = "blue"
		}
	default:
		replicas := shared.IntValue(service["replicas"])
		if replicas <= 0 {
			replicas = shared.IntValue(service["minReplicas"])
		}
		if replicas <= 0 {
			replicas = 1
		}
		details["targetReplicas"] = replicas
	}

	return bson.M{
		"type":      strategyType,
		"phase":     phase,
		"summary":   summary,
		"details":   details,
		"updatedAt": now,
	}
}
