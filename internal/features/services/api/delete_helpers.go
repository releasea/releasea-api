package services

import (
	"context"
	"fmt"
	"strings"

	operations "releaseaapi/internal/features/operations/api"
	operationqueue "releaseaapi/internal/platform/queue"
	"releaseaapi/internal/platform/shared"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func collectServiceEnvironments(ctx context.Context, serviceID string, rules []bson.M) []string {
	envs := make(map[string]struct{})
	for _, rule := range rules {
		env := strings.TrimSpace(shared.StringValue(rule["environment"]))
		if env == "" {
			env = "prod"
		}
		envs[env] = struct{}{}
	}

	deploys, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), bson.M{"serviceId": serviceID})
	if err == nil {
		for _, deploy := range deploys {
			env := strings.TrimSpace(shared.StringValue(deploy["environment"]))
			if env == "" {
				env = "prod"
			}
			envs[env] = struct{}{}
		}
	}

	if len(envs) == 0 {
		return []string{"prod"}
	}

	ordered := make([]string, 0, len(envs))
	for env := range envs {
		ordered = append(ordered, env)
	}
	return ordered
}

func queueRuleDelete(ctx context.Context, rule bson.M, service bson.M, triggeredBy string) error {
	ruleID := ruleIDFromDoc(rule)
	if ruleID == "" {
		return nil
	}

	serviceID := shared.StringValue(rule["serviceId"])
	if serviceID == "" {
		serviceID = shared.StringValue(service["id"])
	}
	if serviceID == "" {
		serviceID = shared.StringValue(service["_id"])
	}
	if serviceID == "" {
		return fmt.Errorf("rule %s missing service id", ruleID)
	}

	environment := strings.TrimSpace(shared.StringValue(rule["environment"]))
	if environment == "" {
		environment = "prod"
	}
	if err := ensureActiveWorkerForEnvironment(ctx, environment, serviceWorkerTags(service)); err != nil {
		return err
	}
	serviceName := shared.StringValue(service["name"])
	if serviceName == "" {
		serviceName = serviceID
	}
	ruleName := shared.StringValue(rule["name"])
	if ruleName == "" {
		ruleName = ruleID
	}
	policyMap := shared.MapPayload(rule["policy"])
	action := shared.StringValue(policyMap["action"])
	if action == "" {
		action = "allow"
	}

	now := shared.NowISO()
	ruleDeployID := "rdeploy-" + uuid.NewString()
	ruleDeployDoc := bson.M{
		"_id":         ruleDeployID,
		"id":          ruleDeployID,
		"ruleId":      ruleID,
		"serviceId":   serviceID,
		"status":      operations.StatusQueued,
		"environment": environment,
		"triggeredBy": triggeredBy,
		"startedAt":   now,
		"logs":        []interface{}{},
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.RuleDeploysCollection), ruleDeployDoc); err != nil {
		return fmt.Errorf("failed to queue rule delete")
	}

	opID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          opID,
		"id":           opID,
		"type":         operations.OperationTypeRuleDelete,
		"resourceType": "rule",
		"resourceId":   ruleID,
		"ruleDeployId": ruleDeployID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment": environment,
			"serviceId":   serviceID,
			"serviceName": serviceName,
			"ruleName":    ruleName,
			"action":      action,
		},
		"requestedBy": triggeredBy,
		"serviceName": serviceName,
	}
	if workerTags := serviceWorkerTags(service); len(workerTags) > 0 {
		shared.MapPayload(opDoc["payload"])["workerTags"] = workerTags
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return fmt.Errorf("failed to queue rule delete")
	}
	_ = shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, bson.M{
		"status":    operations.StatusQueued,
		"updatedAt": now,
	})

	operationqueue.PublishOperationWithDispatchError(ctx, opID)
	return nil
}

func queueServiceDelete(ctx context.Context, service bson.M, environment, triggeredBy string) error {
	serviceID := shared.StringValue(service["id"])
	if serviceID == "" {
		serviceID = shared.StringValue(service["_id"])
	}
	if serviceID == "" {
		return fmt.Errorf("service id missing")
	}
	if environment == "" {
		environment = "prod"
	}
	if err := ensureActiveWorkerForEnvironment(ctx, environment, serviceWorkerTags(service)); err != nil {
		return err
	}
	serviceName := shared.StringValue(service["name"])
	if serviceName == "" {
		serviceName = serviceID
	}

	now := shared.NowISO()
	opID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          opID,
		"id":           opID,
		"type":         operations.OperationTypeServiceDelete,
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment": environment,
		},
		"requestedBy": triggeredBy,
		"serviceName": serviceName,
	}
	if workerTags := serviceWorkerTags(service); len(workerTags) > 0 {
		shared.MapPayload(opDoc["payload"])["workerTags"] = workerTags
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return fmt.Errorf("failed to queue service delete")
	}
	operationqueue.PublishOperationWithDispatchError(ctx, opID)
	return nil
}
