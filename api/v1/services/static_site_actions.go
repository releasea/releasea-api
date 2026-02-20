package services

import (
	"context"
	"errors"
	"strings"

	"releaseaapi/api/v1/operations"
	"releaseaapi/api/v1/shared"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const staticSitePausedGatewaysField = "pausedGateways"

func isStaticSiteService(service bson.M) bool {
	return strings.ToLower(shared.StringValue(service["type"])) == "static-site"
}

func handleStaticSiteStop(ctx context.Context, service bson.M, environment string) (int, error) {
	serviceID := shared.StringValue(service["id"])
	if serviceID == "" {
		serviceID = shared.StringValue(service["_id"])
	}
	if serviceID == "" {
		return 0, errors.New("service id missing")
	}

	rules, err := fetchServiceRules(ctx, serviceID, environment)
	if err != nil {
		return 0, err
	}

	now := shared.NowISO()
	queued := 0

	for _, rule := range rules {
		ruleID := ruleIDFromDoc(rule)
		if ruleID == "" {
			continue
		}
		prevGateways := shared.ToStringSlice(rule["gateways"])
		pausedGateways := shared.ToStringSlice(rule[staticSitePausedGatewaysField])
		if len(pausedGateways) == 0 && len(prevGateways) > 0 {
			pausedGateways = prevGateways
		}

		update := bson.M{
			"gateways":    []string{},
			"status":      operations.StatusQueued,
			"environment": environment,
			"updatedAt":   now,
		}
		if len(pausedGateways) > 0 {
			update[staticSitePausedGatewaysField] = pausedGateways
		}
		if err := shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, update); err != nil {
			return queued, err
		}

		prevStatus := shared.StringValue(rule["status"])
		prevLastPublishedAt := shared.StringValue(rule["lastPublishedAt"])
		if err := operations.QueueRuleDeploy(ctx, ruleID, serviceID, environment, []string{}, prevGateways, prevStatus, prevLastPublishedAt, now); err != nil {
			return queued, err
		}
		queued++
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{
		"status":    "stopped",
		"isActive":  false,
		"updatedAt": now,
	}); err != nil {
		return queued, err
	}

	return queued, nil
}

func handleStaticSiteStart(ctx context.Context, service bson.M, environment string) (int, error) {
	serviceID := shared.StringValue(service["id"])
	if serviceID == "" {
		serviceID = shared.StringValue(service["_id"])
	}
	if serviceID == "" {
		return 0, errors.New("service id missing")
	}

	rules, err := fetchServiceRules(ctx, serviceID, environment)
	if err != nil {
		return 0, err
	}

	now := shared.NowISO()
	queued := 0

	for _, rule := range rules {
		ruleID := ruleIDFromDoc(rule)
		if ruleID == "" {
			continue
		}
		prevGateways := shared.ToStringSlice(rule["gateways"])
		pausedGateways := shared.ToStringSlice(rule[staticSitePausedGatewaysField])
		nextGateways := pausedGateways
		if len(nextGateways) == 0 {
			if len(prevGateways) > 0 {
				nextGateways = prevGateways
			} else {
				nextGateways = operations.BuildGateways(nil, true, false, environment)
			}
		}

		update := bson.M{
			"gateways":    nextGateways,
			"status":      operations.StatusQueued,
			"environment": environment,
			"updatedAt":   now,
		}
		if len(pausedGateways) > 0 {
			update[staticSitePausedGatewaysField] = []string{}
		}
		if err := shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, update); err != nil {
			return queued, err
		}

		prevStatus := shared.StringValue(rule["status"])
		prevLastPublishedAt := shared.StringValue(rule["lastPublishedAt"])
		if err := operations.QueueRuleDeploy(ctx, ruleID, serviceID, environment, nextGateways, prevGateways, prevStatus, prevLastPublishedAt, now); err != nil {
			return queued, err
		}
		queued++
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{
		"status":    "running",
		"isActive":  true,
		"updatedAt": now,
	}); err != nil {
		return queued, err
	}

	return queued, nil
}

func queueStaticSiteDeploy(ctx context.Context, service bson.M, environment, requestedBy string) (bson.M, bson.M, error) {
	serviceID := shared.StringValue(service["id"])
	if serviceID == "" {
		serviceID = shared.StringValue(service["_id"])
	}
	if serviceID == "" {
		return nil, nil, errors.New("service id missing")
	}

	activeFilter := bson.M{
		"serviceId":   serviceID,
		"environment": environment,
		"status": bson.M{
			"$in": operations.DeployQueueBlockingStatuses(),
		},
	}
	existingDeploy, err := shared.FindOne(ctx, shared.Collection(shared.DeploysCollection), activeFilter)
	if err == nil {
		deployID := shared.StringValue(existingDeploy["id"])
		if deployID == "" {
			deployID = shared.StringValue(existingDeploy["_id"])
		}
		if deployID != "" {
			if existingOperation, opErr := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{
				"type":       "service.deploy",
				"resourceId": serviceID,
				"deployId":   deployID,
				"status": bson.M{
					"$in": []string{operations.StatusQueued, operations.StatusInProgress},
				},
			}); opErr == nil {
				return existingOperation, existingDeploy, nil
			}
		}
		return nil, existingDeploy, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) && err != nil {
		return nil, nil, err
	}

	if requestedBy == "" {
		requestedBy = "System"
	}

	deployID := "deploy-" + uuid.NewString()
	now := shared.NowISO()
	branch := shared.StringValue(service["branch"])
	if branch == "" {
		branch = "main"
	}
	deployDoc := bson.M{
		"_id":            deployID,
		"id":             deployID,
		"serviceId":      serviceID,
		"status":         operations.DeployStatusRequested,
		"environment":    environment,
		"commit":         "",
		"branch":         branch,
		"triggeredBy":    requestedBy,
		"startedAt":      now,
		"logs":           []interface{}{},
		"strategyStatus": buildDeployStrategyStatus(service, operations.DeployStatusRequested, "Deployment requested", now),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.DeploysCollection), deployDoc); err != nil {
		return nil, nil, err
	}

	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         "service.deploy",
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment":  environment,
			"version":      "",
			"strategyType": resolveServiceDeployStrategyType(service),
		},
		"deployId":    deployID,
		"requestedBy": requestedBy,
		"serviceName": shared.StringValue(service["name"]),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return nil, nil, err
	}

	shared.PublishOperationWithDispatchError(ctx, operationID)

	return opDoc, deployDoc, nil
}

func fetchServiceRules(ctx context.Context, serviceID, environment string) ([]bson.M, error) {
	filter := bson.M{"serviceId": serviceID}
	if environment != "" {
		filter["environment"] = environment
	}
	items, err := shared.FindAll(ctx, shared.Collection(shared.RulesCollection), filter)
	if err != nil {
		return nil, err
	}
	return items, nil
}

func ruleIDFromDoc(rule bson.M) string {
	if ruleID := shared.StringValue(rule["id"]); ruleID != "" {
		return ruleID
	}
	return shared.StringValue(rule["_id"])
}
