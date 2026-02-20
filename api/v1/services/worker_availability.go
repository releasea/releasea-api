package services

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	workerAvailabilityErrorCode = "WORKER_UNAVAILABLE_FOR_ENVIRONMENT"
	defaultWorkerStaleSeconds   = 90
	defaultOperationEnvironment = "prod"
)

var activeWorkerStatuses = []string{"online", "busy", "pending"}

type workerAvailabilityError struct {
	Environment string
}

func (e workerAvailabilityError) Error() string {
	return workerUnavailableMessage(e.Environment)
}

func normalizeOperationEnvironment(value string) string {
	environment := strings.TrimSpace(value)
	if environment == "" {
		environment = defaultOperationEnvironment
	}
	return environment
}

func workerUnavailableMessage(environment string) string {
	normalized := normalizeOperationEnvironment(environment)
	return fmt.Sprintf("No active worker available for %s environment", normalized)
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
	lastHeartbeat := strings.TrimSpace(shared.StringValue(worker["lastHeartbeat"]))
	if lastHeartbeat == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, lastHeartbeat)
	if err != nil {
		return false
	}
	return !parsed.Before(threshold)
}

func hasActiveWorkerForEnvironment(ctx context.Context, environment string) (bool, error) {
	normalizedEnvironment := normalizeOperationEnvironment(environment)
	heartbeatThreshold := time.Now().UTC().Add(-time.Duration(workerStaleSeconds()) * time.Second)

	workers, err := shared.FindAll(ctx, shared.Collection(shared.WorkersCollection), bson.M{
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
		workerEnvironment := strings.TrimSpace(shared.StringValue(worker["environment"]))
		if workerEnvironment == "" {
			continue
		}
		if !shared.EnvironmentsShareNamespace(workerEnvironment, normalizedEnvironment) {
			continue
		}
		if onlineAgents, ok := worker["onlineAgents"]; ok && shared.IntValue(onlineAgents) <= 0 {
			continue
		}
		if !isWorkerHeartbeatFresh(worker, heartbeatThreshold) {
			continue
		}
		return true, nil
	}
	return false, nil
}

func ensureActiveWorkerForEnvironment(ctx context.Context, environment string) error {
	active, err := hasActiveWorkerForEnvironment(ctx, environment)
	if err != nil {
		return err
	}
	if !active {
		return workerAvailabilityError{Environment: normalizeOperationEnvironment(environment)}
	}
	return nil
}

func isWorkerAvailabilityError(err error) bool {
	var availabilityErr workerAvailabilityError
	return errors.As(err, &availabilityErr)
}

func respondWorkerAvailabilityError(c *gin.Context, environment string) {
	normalizedEnvironment := normalizeOperationEnvironment(environment)
	c.JSON(http.StatusConflict, gin.H{
		"message":     workerUnavailableMessage(normalizedEnvironment),
		"code":        workerAvailabilityErrorCode,
		"environment": normalizedEnvironment,
	})
}

func ensureWorkerAvailabilityOrRespond(c *gin.Context, ctx context.Context, environment string) bool {
	if err := ensureActiveWorkerForEnvironment(ctx, environment); err != nil {
		if isWorkerAvailabilityError(err) {
			respondWorkerAvailabilityError(c, environment)
			return false
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to validate worker availability")
		return false
	}
	return true
}
