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
	return WorkerUnavailableMessageWithTags(environment, nil)
}

func WorkerUnavailableMessageWithTags(environment string, requiredTags []string) string {
	normalizedTags := NormalizeWorkerTags(requiredTags)
	if len(normalizedTags) == 0 {
		return fmt.Sprintf(
			"No active worker available for %s environment",
			NormalizeOperationEnvironment(environment),
		)
	}
	return fmt.Sprintf(
		"No active worker available for %s environment matching tags: %s",
		NormalizeOperationEnvironment(environment),
		strings.Join(normalizedTags, ", "),
	)
}

func NormalizeWorkerTags(tags []string) []string {
	normalized := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}
	return normalized
}

func WorkerSatisfiesEnvironmentAndTags(worker bson.M, environment string, requiredTags []string, heartbeatThreshold time.Time) bool {
	workerEnvironment := strings.TrimSpace(StringValue(worker["environment"]))
	if workerEnvironment == "" {
		return false
	}
	if !EnvironmentsShareNamespace(workerEnvironment, NormalizeOperationEnvironment(environment)) {
		return false
	}
	if onlineAgents, ok := worker["onlineAgents"]; ok && IntValue(onlineAgents) <= 0 {
		return false
	}
	if !isWorkerHeartbeatFresh(worker, heartbeatThreshold) {
		return false
	}

	requiredTags = NormalizeWorkerTags(requiredTags)
	if len(requiredTags) == 0 {
		return true
	}

	availableTags := make(map[string]struct{}, len(requiredTags))
	for _, tag := range NormalizeWorkerTags(ToStringSlice(worker["tags"])) {
		availableTags[tag] = struct{}{}
	}
	for _, tag := range requiredTags {
		if _, ok := availableTags[tag]; !ok {
			return false
		}
	}
	return true
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
	return HasActiveWorkerForEnvironmentAndTags(ctx, environment, nil)
}

func HasActiveWorkerForEnvironmentAndTags(ctx context.Context, environment string, requiredTags []string) (bool, error) {
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
		if WorkerSatisfiesEnvironmentAndTags(worker, normalizedEnvironment, requiredTags, heartbeatThreshold) {
			return true, nil
		}
	}
	return false, nil
}
