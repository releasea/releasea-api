package services

import (
	"context"
	"errors"
	"net/http"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

const (
	workerAvailabilityErrorCode = shared.WorkerAvailabilityErrorCode
)

type workerAvailabilityError struct {
	Environment string
	Tags        []string
}

func (e workerAvailabilityError) Error() string {
	return shared.WorkerUnavailableMessageWithTags(e.Environment, e.Tags)
}

func ensureActiveWorkerForEnvironment(ctx context.Context, environment string, requiredTags []string) error {
	active, err := shared.HasActiveWorkerForEnvironmentAndTags(ctx, environment, requiredTags)
	if err != nil {
		return err
	}
	if !active {
		return workerAvailabilityError{
			Environment: shared.NormalizeOperationEnvironment(environment),
			Tags:        shared.NormalizeWorkerTags(requiredTags),
		}
	}
	return nil
}

func isWorkerAvailabilityError(err error) bool {
	var availabilityErr workerAvailabilityError
	return errors.As(err, &availabilityErr)
}

func respondWorkerAvailabilityError(c *gin.Context, environment string, requiredTags []string) {
	normalizedEnvironment := shared.NormalizeOperationEnvironment(environment)
	normalizedTags := shared.NormalizeWorkerTags(requiredTags)
	c.JSON(http.StatusConflict, gin.H{
		"message":     shared.WorkerUnavailableMessageWithTags(normalizedEnvironment, normalizedTags),
		"code":        workerAvailabilityErrorCode,
		"environment": normalizedEnvironment,
		"workerTags":  normalizedTags,
	})
}

func ensureWorkerAvailabilityOrRespond(c *gin.Context, ctx context.Context, environment string, requiredTags []string) bool {
	if err := ensureActiveWorkerForEnvironment(ctx, environment, requiredTags); err != nil {
		if isWorkerAvailabilityError(err) {
			respondWorkerAvailabilityError(c, environment, requiredTags)
			return false
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to validate worker availability")
		return false
	}
	return true
}
