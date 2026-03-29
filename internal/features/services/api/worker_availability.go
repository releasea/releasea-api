package services

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

const (
	workerAvailabilityErrorCode = shared.WorkerAvailabilityErrorCode
)

type workerAvailabilityError struct {
	Environment string
	Tags        []string
	Cluster     string
}

func (e workerAvailabilityError) Error() string {
	if e.Cluster != "" {
		return shared.WorkerUnavailableMessageWithTags(e.Environment, append(shared.NormalizeWorkerTags(e.Tags), "cluster:"+e.Cluster))
	}
	return shared.WorkerUnavailableMessageWithTags(e.Environment, e.Tags)
}

func ensureActiveWorkerForEnvironment(ctx context.Context, environment string, requiredTags []string) error {
	return ensureActiveWorkerForEnvironmentWithCluster(ctx, environment, requiredTags, "")
}

func ensureActiveWorkerForEnvironmentWithCluster(ctx context.Context, environment string, requiredTags []string, cluster string) error {
	active, err := shared.HasActiveWorkerForEnvironmentAndTags(ctx, environment, requiredTags)
	if cluster != "" {
		active, err = shared.HasActiveWorkerForEnvironmentTagsAndCluster(ctx, environment, requiredTags, cluster)
	}
	if err != nil {
		return err
	}
	if !active {
		return workerAvailabilityError{
			Environment: shared.NormalizeOperationEnvironment(environment),
			Tags:        shared.NormalizeWorkerTags(requiredTags),
			Cluster:     strings.TrimSpace(cluster),
		}
	}
	return nil
}

func isWorkerAvailabilityError(err error) bool {
	var availabilityErr workerAvailabilityError
	return errors.As(err, &availabilityErr)
}

func respondWorkerAvailabilityError(c *gin.Context, environment string, requiredTags []string) {
	respondWorkerAvailabilityErrorWithCluster(c, environment, requiredTags, "")
}

func respondWorkerAvailabilityErrorWithCluster(c *gin.Context, environment string, requiredTags []string, cluster string) {
	normalizedEnvironment := shared.NormalizeOperationEnvironment(environment)
	normalizedTags := shared.NormalizeWorkerTags(requiredTags)
	c.JSON(http.StatusConflict, gin.H{
		"message":                workerAvailabilityError{Environment: normalizedEnvironment, Tags: normalizedTags, Cluster: strings.TrimSpace(cluster)}.Error(),
		"code":                   workerAvailabilityErrorCode,
		"environment":            normalizedEnvironment,
		"workerTags":             normalizedTags,
		"preferredWorkerCluster": strings.TrimSpace(cluster),
	})
}

func ensureWorkerAvailabilityOrRespond(c *gin.Context, ctx context.Context, environment string, requiredTags []string) bool {
	return ensureWorkerAvailabilityOrRespondWithCluster(c, ctx, environment, requiredTags, "")
}

func ensureWorkerAvailabilityOrRespondWithCluster(c *gin.Context, ctx context.Context, environment string, requiredTags []string, cluster string) bool {
	if err := ensureActiveWorkerForEnvironmentWithCluster(ctx, environment, requiredTags, cluster); err != nil {
		if isWorkerAvailabilityError(err) {
			respondWorkerAvailabilityErrorWithCluster(c, environment, requiredTags, cluster)
			return false
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to validate worker availability")
		return false
	}
	return true
}
