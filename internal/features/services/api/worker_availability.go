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
}

func (e workerAvailabilityError) Error() string {
	return shared.WorkerUnavailableMessage(e.Environment)
}

func ensureActiveWorkerForEnvironment(ctx context.Context, environment string) error {
	active, err := shared.HasActiveWorkerForEnvironment(ctx, environment)
	if err != nil {
		return err
	}
	if !active {
		return workerAvailabilityError{Environment: shared.NormalizeOperationEnvironment(environment)}
	}
	return nil
}

func isWorkerAvailabilityError(err error) bool {
	var availabilityErr workerAvailabilityError
	return errors.As(err, &availabilityErr)
}

func respondWorkerAvailabilityError(c *gin.Context, environment string) {
	normalizedEnvironment := shared.NormalizeOperationEnvironment(environment)
	c.JSON(http.StatusConflict, gin.H{
		"message":     shared.WorkerUnavailableMessage(normalizedEnvironment),
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
