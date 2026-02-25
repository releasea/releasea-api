package shared

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	defaultOperationEnvironment = "prod"
	defaultWorkerStaleSeconds   = 90

	WorkerAvailabilityErrorCode = "WORKER_UNAVAILABLE_FOR_ENVIRONMENT"
)

var activeWorkerStatuses = []string{"online", "busy", "pending"}

func NormalizeOperationEnvironment(value string) string {
	environment := strings.TrimSpace(value)
	if environment == "" {
		environment = defaultOperationEnvironment
	}
	return environment
}

func WorkerUnavailableMessage(environment string) string {
	return fmt.Sprintf(
		"No active worker available for %s environment",
		NormalizeOperationEnvironment(environment),
	)
}

func workerStaleSeconds() int {
	if value := strings.TrimSpace(os.Getenv("WORKER_STALE_SECONDS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultWorkerStaleSeconds
}

func isWorkerHeartbeatFresh(worker bson.M, threshold time.Time) bool {
	lastHeartbeat := strings.TrimSpace(StringValue(worker["lastHeartbeat"]))
	if lastHeartbeat == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, lastHeartbeat)
	if err != nil {
		return false
	}
	return !parsed.Before(threshold)
}

func HasActiveWorkerForEnvironment(ctx context.Context, environment string) (bool, error) {
	normalizedEnvironment := NormalizeOperationEnvironment(environment)
	heartbeatThreshold := time.Now().UTC().Add(-time.Duration(workerStaleSeconds()) * time.Second)

	workers, err := FindAll(ctx, Collection(WorkersCollection), bson.M{
		"status": bson.M{
			"$in": activeWorkerStatuses,
		},
		"lastHeartbeat": bson.M{
			"$gte": heartbeatThreshold.Format(time.RFC3339),
		},
	})
	if err != nil {
		return false, err
	}

	for _, worker := range workers {
		workerEnvironment := strings.TrimSpace(StringValue(worker["environment"]))
		if workerEnvironment == "" {
			continue
		}
		if !EnvironmentsShareNamespace(workerEnvironment, normalizedEnvironment) {
			continue
		}
		if onlineAgents, ok := worker["onlineAgents"]; ok && IntValue(onlineAgents) <= 0 {
			continue
		}
		if !isWorkerHeartbeatFresh(worker, heartbeatThreshold) {
			continue
		}
		return true, nil
	}
	return false, nil
}
