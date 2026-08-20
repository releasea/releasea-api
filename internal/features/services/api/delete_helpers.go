package services

import (
	"context"
	"fmt"
	"sort"
	"strings"

	operations "releaseaapi/internal/features/operations/api"
	operationqueue "releaseaapi/internal/platform/queue"
	"releaseaapi/internal/platform/shared"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func collectServiceEnvironments(ctx context.Context, serviceID string, rules []bson.M) ([]string, error) {
	deploys, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), bson.M{"serviceId": serviceID})
	if err != nil {
		return nil, err
	}
	return collectServiceEnvironmentsFromDocuments(rules, deploys), nil
}

func collectServiceEnvironmentsFromDocuments(rules, deploys []bson.M) []string {
	namespaces := make(map[string]struct{})
	for _, rule := range rules {
		env := strings.TrimSpace(shared.StringValue(rule["environment"]))
		if env == "" {
			env = "prod"
		}
		namespaces[shared.ResolveAppNamespace(env)] = struct{}{}
	}

	for _, deploy := range deploys {
		env := strings.TrimSpace(shared.StringValue(deploy["environment"]))
		if env == "" {
			env = "prod"
		}
		namespaces[shared.ResolveAppNamespace(env)] = struct{}{}
	}

	if len(namespaces) == 0 {
		return nil
	}

	ordered := make([]string, 0, len(namespaces))
	for namespace := range namespaces {
		ordered = append(ordered, canonicalEnvironmentForNamespace(namespace))
	}
	sort.Strings(ordered)
	return ordered
}

func canonicalEnvironmentForNamespace(namespace string) string {
	switch namespace {
	case shared.NamespaceProduction:
		return "prod"
	case shared.NamespaceStaging:
		return "staging"
	default:
		return "dev"
	}
}

func serviceDeleteWorkerRouting(service bson.M) workerRoutingResolution {
	// Cleanup only requires Kubernetes access. Build/GPU/region tags must not
	// strand destructive, idempotent operations when the original worker is gone.
	return workerRoutingResolution{
		PreferredWorkerCluster: strings.TrimSpace(shared.StringValue(service["preferredWorkerCluster"])),
	}
}

func queueServiceDelete(ctx context.Context, service bson.M, environment, triggeredBy, deletionRequestID string) error {
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
	workerRouting := serviceDeleteWorkerRouting(service)
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
			"environment":       environment,
			"deletionRequestId": deletionRequestID,
		},
		"requestedBy": triggeredBy,
		"serviceName": serviceName,
	}
	applyWorkerRoutingToPayload(shared.MapPayload(opDoc["payload"]), workerRouting)
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return fmt.Errorf("failed to queue service delete")
	}
	operationqueue.PublishOperationWithDispatchError(ctx, opID)
	return nil
}
