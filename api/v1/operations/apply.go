package operations

import (
	"context"
	"strings"

	"releaseaapi/api/v1/shared"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func applyOperationStart(ctx context.Context, op bson.M, now string) {
	switch shared.StringValue(op["type"]) {
	case "service.deploy":
		deployID := shared.StringValue(op["deployId"])
		if deployID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.DeploysCollection), deployID, bson.M{
				"status":                   DeployStatusScheduled,
				"strategyStatus.phase":     DeployStatusScheduled,
				"strategyStatus.summary":   "Deployment scheduled",
				"strategyStatus.updatedAt": now,
			})
		}
		serviceID := shared.StringValue(op["resourceId"])
		if serviceID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "pending", "isActive": false})
		}
	case "service.scale", "service.restart", "service.promote-canary":
		serviceID := shared.StringValue(op["resourceId"])
		if serviceID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "pending", "updatedAt": now})
		}
	case "service.delete":
		serviceID := shared.StringValue(op["resourceId"])
		if serviceID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "deleting", "isActive": false, "updatedAt": now})
		}
	case "rule.deploy", "rule.publish":
		setRuleDeployStatus(ctx, shared.StringValue(op["ruleDeployId"]), StatusInProgress, "")
		ruleID := shared.StringValue(op["resourceId"])
		if ruleID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, bson.M{"status": "in-progress", "updatedAt": now})
		}
	case "rule.delete":
		setRuleDeployStatus(ctx, shared.StringValue(op["ruleDeployId"]), StatusInProgress, "")
	}
}

func applyOperationSuccess(ctx context.Context, op bson.M, now string) error {
	opType := shared.StringValue(op["type"])
	switch opType {
	case "service.deploy":
		deployID := shared.StringValue(op["deployId"])
		if deployID != "" {
			if err := shared.UpdateByID(ctx, shared.Collection(shared.DeploysCollection), deployID, bson.M{
				"status":                   DeployStatusCompleted,
				"finishedAt":               now,
				"strategyStatus.phase":     DeployStatusCompleted,
				"strategyStatus.summary":   "New version active",
				"strategyStatus.updatedAt": now,
			}); err != nil {
				return err
			}
		}
		serviceID := shared.StringValue(op["resourceId"])
		payload := shared.MapPayload(op["payload"])
		if serviceID != "" {
			service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
			if err == nil {
				serviceType := strings.ToLower(shared.StringValue(service["type"]))
				templateID := strings.ToLower(shared.StringValue(service["deployTemplateId"]))
				firstSuccessfulDeploy := strings.TrimSpace(shared.StringValue(service["lastDeployAt"])) == ""
				nextStatus := "pending"
				if serviceType == "static-site" {
					nextStatus = "running"
				} else if templateID == "tpl-cronjob" {
					nextStatus = "created"
				}
				_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{
					"lastDeployAt": now,
					"status":       nextStatus,
					"isActive":     true,
				})

				environment := shared.StringValue(shared.MapPayload(op["payload"])["environment"])
				if environment == "" {
					environment = "prod"
				}
				if serviceType == "microservice" {
					if firstSuccessfulDeploy {
						_ = ensureDefaultRule(ctx, serviceID, environment, now)
					}
					servicePort := shared.IntValue(service["port"])
					_ = SyncRulesToServicePort(ctx, serviceID, servicePort)
					if strings.EqualFold(shared.StringValue(payload["strategyType"]), "canary") {
						_ = RepublishRulesForServiceStrategyWithOptions(ctx, serviceID, RuleStrategyRepublishOptions{
							Environment: environment,
						})
					}
				}
			}
		}
	case "rule.deploy", "rule.publish":
		setRuleDeployStatus(ctx, shared.StringValue(op["ruleDeployId"]), "success", now)
		payload := shared.MapPayload(op["payload"])
		internal := shared.BoolValue(payload["internal"])
		external := shared.BoolValue(payload["external"])
		environment := shared.StringValue(payload["environment"])
		status := "draft"
		if internal || external {
			status = "published"
		}
		nextGateways := shared.ToStringSlice(payload["nextGateways"])
		if len(nextGateways) == 0 && (internal || external) {
			nextGateways = BuildGateways(nil, internal, external, environment)
		}
		update := bson.M{
			"status":    status,
			"gateways":  nextGateways,
			"updatedAt": now,
		}
		if status == "published" {
			update["lastPublishedAt"] = now
		} else {
			update["lastPublishedAt"] = nil
		}
		return shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), shared.StringValue(op["resourceId"]), update)
	case "rule.delete":
		setRuleDeployStatus(ctx, shared.StringValue(op["ruleDeployId"]), "success", now)
		ruleID := shared.StringValue(op["resourceId"])
		if ruleID != "" {
			_ = shared.DeleteByID(ctx, shared.Collection(shared.RulesCollection), ruleID)
		}
		return nil
	case "service.scale":
		payload := shared.MapPayload(op["payload"])
		replicas := shared.IntValue(payload["replicas"])
		status := "running"
		isActive := true
		if replicas <= 0 {
			status = "stopped"
			isActive = false
		}
		update := bson.M{
			"status":    status,
			"isActive":  isActive,
			"updatedAt": now,
		}
		return shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), shared.StringValue(op["resourceId"]), update)
	case "service.restart":
		payload := shared.MapPayload(op["payload"])
		update := bson.M{
			"updatedAt": now,
		}
		if prevStatus := shared.StringValue(payload["prevStatus"]); prevStatus != "" {
			update["status"] = prevStatus
		} else {
			update["status"] = "running"
		}
		if _, ok := payload["prevIsActive"]; ok {
			update["isActive"] = shared.BoolValue(payload["prevIsActive"])
		} else {
			update["isActive"] = true
		}
		return shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), shared.StringValue(op["resourceId"]), update)
	case "service.promote-canary":
		serviceID := shared.StringValue(op["resourceId"])
		payload := shared.MapPayload(op["payload"])
		environment := strings.TrimSpace(shared.StringValue(payload["environment"]))
		if environment == "" {
			environment = "prod"
		}
		stableCanaryPercent := 0
		if serviceID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "running", "isActive": true, "updatedAt": now})
			_ = RepublishRulesForServiceStrategyWithOptions(ctx, serviceID, RuleStrategyRepublishOptions{
				Environment:           environment,
				CanaryPercentOverride: &stableCanaryPercent,
			})
		}
		return nil
	case "service.delete":
		serviceID := shared.StringValue(op["resourceId"])
		if serviceID == "" {
			return nil
		}
		pending, err := shared.Collection(shared.OperationsCollection).CountDocuments(ctx, bson.M{
			"type":       "service.delete",
			"resourceId": serviceID,
			"status": bson.M{
				"$in": []string{StatusQueued, StatusInProgress},
			},
		})
		if err != nil {
			return err
		}
		if pending > 0 {
			return nil
		}
		_, _ = shared.Collection(shared.RuleDeploysCollection).DeleteMany(ctx, bson.M{"serviceId": serviceID})
		_, _ = shared.Collection(shared.DeploysCollection).DeleteMany(ctx, bson.M{"serviceId": serviceID})
		_, _ = shared.Collection(shared.LogsCollection).DeleteMany(ctx, bson.M{"serviceId": serviceID})
		_, _ = shared.Collection(shared.RulesCollection).DeleteMany(ctx, bson.M{"serviceId": serviceID})
		_, _ = shared.Collection(shared.ExternalEndpointsCollection).DeleteMany(ctx, bson.M{"serviceId": serviceID})
		return shared.DeleteByID(ctx, shared.Collection(shared.ServicesCollection), serviceID)
	}
	return nil
}

func ensureDefaultRule(ctx context.Context, serviceID, environment, now string) error {
	if serviceID == "" {
		return nil
	}
	if environment == "" {
		environment = "prod"
	}

	existingCount, err := shared.Collection(shared.RulesCollection).CountDocuments(ctx, bson.M{
		"serviceId":   serviceID,
		"environment": environment,
	})
	if err != nil {
		return err
	}
	if existingCount > 0 {
		return nil
	}

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		return err
	}
	if strings.ToLower(shared.StringValue(service["type"])) != "microservice" {
		return nil
	}

	ruleName := shared.DefaultRuleName
	port := shared.IntValue(service["port"])
	if port <= 0 {
		port = 80
	}

	gateways := BuildGateways(nil, true, false, environment)

	ruleID := "rule-" + uuid.NewString()
	ruleDoc := bson.M{
		"_id":         ruleID,
		"id":          ruleID,
		"name":        ruleName,
		"serviceId":   serviceID,
		"environment": environment,
		"hosts":       []string{},
		"gateways":    gateways,
		"paths":       []string{"/"},
		"methods":     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"},
		"protocol":    "https",
		"port":        port,
		"status":      StatusQueued,
		"createdAt":   now,
		"updatedAt":   now,
		"policy":      bson.M{"action": "allow", "timeoutMs": 1500, "retries": 2, "ipPolicy": "open"},
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.RulesCollection), ruleDoc); err != nil {
		return err
	}
	return QueueRuleDeploy(ctx, ruleID, serviceID, environment, gateways, []string{}, "draft", "", now)
}

func RepublishRulesForServicePortChange(ctx context.Context, serviceID string, newPort int) error {
	if serviceID == "" || newPort <= 0 {
		return nil
	}
	rules, err := shared.FindAll(ctx, shared.Collection(shared.RulesCollection), bson.M{"serviceId": serviceID})
	if err != nil {
		return err
	}
	now := shared.NowISO()
	for _, rule := range rules {
		ruleID := shared.StringValue(rule["id"])
		if ruleID == "" {
			continue
		}
		currentPort := shared.IntValue(rule["port"])
		if currentPort == newPort {
			continue
		}
		prevStatus := shared.StringValue(rule["status"])
		prevLastPublishedAt := shared.StringValue(rule["lastPublishedAt"])
		gateways := shared.ToStringSlice(rule["gateways"])
		shouldRepublish := len(gateways) > 0 || prevStatus == "published" || prevStatus == "publishing" || prevStatus == StatusQueued || prevStatus == "in-progress"

		update := bson.M{
			"port":      newPort,
			"updatedAt": now,
		}
		if shouldRepublish {
			update["status"] = StatusQueued
		}
		if err := shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, update); err != nil {
			continue
		}
		if shouldRepublish {
			ruleEnv := shared.StringValue(rule["environment"])
			if ruleEnv == "" {
				ruleEnv = "prod"
			}
			if err := QueueRuleDeploy(ctx, ruleID, serviceID, ruleEnv, gateways, gateways, prevStatus, prevLastPublishedAt, now); err != nil {
				continue
			}
		}
	}
	return nil
}

func SyncRulesToServicePort(ctx context.Context, serviceID string, servicePort int) error {
	if serviceID == "" || servicePort <= 0 {
		return nil
	}
	return RepublishRulesForServicePortChange(ctx, serviceID, servicePort)
}

type RuleStrategyRepublishOptions struct {
	Environment           string
	CanaryPercentOverride *int
}

func RepublishRulesForServiceStrategy(ctx context.Context, serviceID string) error {
	return RepublishRulesForServiceStrategyWithOptions(ctx, serviceID, RuleStrategyRepublishOptions{})
}

func RepublishRulesForServiceStrategyWithOptions(ctx context.Context, serviceID string, options RuleStrategyRepublishOptions) error {
	if serviceID == "" {
		return nil
	}
	targetEnvironment := strings.TrimSpace(options.Environment)
	rules, err := shared.FindAll(ctx, shared.Collection(shared.RulesCollection), bson.M{"serviceId": serviceID})
	if err != nil {
		return err
	}
	now := shared.NowISO()
	for _, rule := range rules {
		ruleID := shared.StringValue(rule["id"])
		if ruleID == "" {
			continue
		}
		prevStatus := shared.StringValue(rule["status"])
		if prevStatus == StatusQueued || prevStatus == StatusInProgress {
			continue
		}
		gateways := shared.ToStringSlice(rule["gateways"])
		if len(gateways) == 0 && prevStatus != "published" && prevStatus != "publishing" {
			continue
		}
		ruleEnv := shared.StringValue(rule["environment"])
		if ruleEnv == "" {
			ruleEnv = "prod"
		}
		if targetEnvironment != "" && ruleEnv != targetEnvironment {
			continue
		}
		prevLastPublishedAt := shared.StringValue(rule["lastPublishedAt"])
		update := bson.M{
			"status":    StatusQueued,
			"updatedAt": now,
		}
		if err := shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, update); err != nil {
			continue
		}
		if err := QueueRuleDeployWithOptions(ctx, ruleID, serviceID, ruleEnv, gateways, gateways, prevStatus, prevLastPublishedAt, now, RuleDeployQueueOptions{
			CanaryPercentOverride: options.CanaryPercentOverride,
		}); err != nil {
			continue
		}
	}
	return nil
}

type RuleDeployQueueOptions struct {
	CanaryPercentOverride *int
}

func QueueRuleDeploy(ctx context.Context, ruleID, serviceID, environment string, nextGateways, prevGateways []string, prevStatus, prevLastPublishedAt, now string) error {
	return QueueRuleDeployWithOptions(ctx, ruleID, serviceID, environment, nextGateways, prevGateways, prevStatus, prevLastPublishedAt, now, RuleDeployQueueOptions{})
}

func QueueRuleDeployWithOptions(
	ctx context.Context,
	ruleID,
	serviceID,
	environment string,
	nextGateways,
	prevGateways []string,
	prevStatus,
	prevLastPublishedAt,
	now string,
	options RuleDeployQueueOptions,
) error {
	internal := false
	external := false
	internalGateway := shared.EnvOrDefault("RELEASEA_INTERNAL_GATEWAY", "istio-system/releasea-internal-gateway")
	for _, gateway := range nextGateways {
		if gateway == internalGateway {
			internal = true
		} else if gateway != "" {
			external = true
		}
	}
	if environment == "" {
		environment = "prod"
	}

	ruleDeployID := "rdeploy-" + uuid.NewString()
	ruleDeployDoc := bson.M{
		"_id":         ruleDeployID,
		"id":          ruleDeployID,
		"ruleId":      ruleID,
		"serviceId":   serviceID,
		"status":      StatusQueued,
		"environment": environment,
		"triggeredBy": "System",
		"startedAt":   now,
		"logs":        []interface{}{},
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.RuleDeploysCollection), ruleDeployDoc); err != nil {
		return err
	}

	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         "rule.deploy",
		"resourceType": "rule",
		"resourceId":   ruleID,
		"ruleDeployId": ruleDeployID,
		"status":       StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"internal":            internal,
			"external":            external,
			"environment":         environment,
			"prevGateways":        prevGateways,
			"nextGateways":        nextGateways,
			"prevStatus":          prevStatus,
			"prevLastPublishedAt": prevLastPublishedAt,
		},
		"requestedBy": "System",
	}
	if options.CanaryPercentOverride != nil {
		shared.MapPayload(opDoc["payload"])["canaryPercentOverride"] = *options.CanaryPercentOverride
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return err
	}
	shared.PublishOperationWithDispatchError(ctx, operationID)
	return nil
}

func applyOperationFailure(ctx context.Context, op bson.M, now string) error {
	opType := shared.StringValue(op["type"])
	switch opType {
	case "service.deploy":
		deployID := shared.StringValue(op["deployId"])
		deployStatus := DeployStatusFailed
		phase := DeployStatusFailed
		summary := "Deployment failed"
		if deployID != "" {
			if currentDeploy, err := shared.FindOne(ctx, shared.Collection(shared.DeploysCollection), bson.M{"id": deployID}); err == nil {
				if NormalizeDeployStatus(shared.StringValue(currentDeploy["status"])) == DeployStatusRollback {
					deployStatus = DeployStatusRollback
					phase = DeployStatusRollback
					summary = "Rollback completed. Previous version restored"
				}
			}
		}
		if deployID != "" {
			if err := shared.UpdateByID(ctx, shared.Collection(shared.DeploysCollection), deployID, bson.M{
				"status":                   deployStatus,
				"finishedAt":               now,
				"strategyStatus.phase":     phase,
				"strategyStatus.summary":   summary,
				"strategyStatus.updatedAt": now,
			}); err != nil {
				return err
			}
		}
		serviceID := shared.StringValue(op["resourceId"])
		if serviceID != "" {
			if deployStatus == DeployStatusRollback {
				_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "running", "isActive": true, "updatedAt": now})
			} else {
				_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "error", "isActive": false})
			}
		}
		return nil
	case "service.delete":
		serviceID := shared.StringValue(op["resourceId"])
		if serviceID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "error", "isActive": false, "updatedAt": now})
		}
		return nil
	case "rule.deploy", "rule.publish":
		setRuleDeployStatus(ctx, shared.StringValue(op["ruleDeployId"]), "failed", now)
		payload := shared.MapPayload(op["payload"])
		prevGateways := shared.ToStringSlice(payload["prevGateways"])
		prevStatus := shared.StringValue(payload["prevStatus"])
		prevLastPublishedAt := shared.StringValue(payload["prevLastPublishedAt"])
		update := bson.M{
			"status":    prevStatus,
			"gateways":  prevGateways,
			"updatedAt": now,
		}
		if prevLastPublishedAt != "" {
			update["lastPublishedAt"] = prevLastPublishedAt
		} else {
			update["lastPublishedAt"] = nil
		}
		return shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), shared.StringValue(op["resourceId"]), update)
	case "rule.delete":
		setRuleDeployStatus(ctx, shared.StringValue(op["ruleDeployId"]), "failed", now)
		return nil
	case "service.promote-canary":
		serviceID := shared.StringValue(op["resourceId"])
		if serviceID != "" {
			_ = shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{"status": "error", "isActive": false, "updatedAt": now})
		}
		return nil
	case "service.scale", "service.restart":
		payload := shared.MapPayload(op["payload"])
		prevStatus := shared.StringValue(payload["prevStatus"])
		update := bson.M{
			"updatedAt": now,
		}
		if prevStatus != "" {
			update["status"] = prevStatus
		}
		if _, ok := payload["prevIsActive"]; ok {
			update["isActive"] = shared.BoolValue(payload["prevIsActive"])
		}
		return shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), shared.StringValue(op["resourceId"]), update)
	}
	return nil
}

func BuildGateways(existing []string, internal bool, external bool, environment string) []string {
	internalGateway := shared.EnvOrDefault("RELEASEA_INTERNAL_GATEWAY", "istio-system/releasea-internal-gateway")
	externalGateway := shared.EnvOrDefault("RELEASEA_EXTERNAL_GATEWAY", "istio-system/releasea-external-gateway")
	externalGateways := make([]string, 0)
	for _, gateway := range existing {
		if gateway == "" || gateway == "mesh" || gateway == internalGateway {
			continue
		}
		externalGateways = append(externalGateways, gateway)
	}
	next := make([]string, 0)
	if internal {
		next = append(next, internalGateway)
	}
	if external {
		if len(externalGateways) > 0 {
			next = append(next, externalGateways...)
		} else {
			next = append(next, externalGateway)
		}
	}
	return shared.UniqueStrings(next)
}

func setRuleDeployStatus(ctx context.Context, ruleDeployID, status, finishedAt string) {
	ruleDeployID = strings.TrimSpace(ruleDeployID)
	if ruleDeployID == "" {
		return
	}
	update := bson.M{
		"status": status,
	}
	if strings.TrimSpace(finishedAt) != "" {
		update["finishedAt"] = finishedAt
	}
	_ = shared.UpdateByID(ctx, shared.Collection(shared.RuleDeploysCollection), ruleDeployID, update)
}
