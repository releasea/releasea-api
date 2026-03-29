package observability

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	operations "releaseaapi/internal/features/operations/api"
	platformmodels "releaseaapi/internal/platform/models"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	controlPlaneMetricsVersion          = "1"
	controlPlaneStaleQueueThreshold     = time.Minute
	controlPlaneRecentFailureWindow     = 24 * time.Hour
	controlPlaneQueueStatusHealthy      = "healthy"
	controlPlaneQueueStatusReview       = "review"
	controlPlaneQueueStatusDegraded     = "degraded"
	controlPlaneDispatchStatusSending   = "dispatching"
	controlPlaneDispatchStatusFailed    = "dispatch-failed"
	defaultWorkerDeadLetterQueueNameSfx = ".dead-letter"
)

var controlPlaneMetricsLoader = loadControlPlaneMetrics

func GetControlPlaneMetrics(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()

	payload, err := controlPlaneMetricsLoader(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load control plane metrics")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func loadControlPlaneMetrics(ctx context.Context) (platformmodels.ControlPlaneMetrics, error) {
	queueName := resolveQueueName()
	dlqEnabled := resolveDeadLetterEnabled()
	dlqName := resolveDeadLetterQueueName(queueName)

	queueMetrics := platformmodels.ControlPlaneQueueMetrics{
		QueueName:           queueName,
		DeadLetterEnabled:   dlqEnabled,
		DeadLetterQueueName: dlqName,
	}

	operationsCol := shared.Collection(shared.OperationsCollection)
	now := time.Now().UTC()
	staleThreshold := now.Add(-controlPlaneStaleQueueThreshold).Format(time.RFC3339)
	recentFailureThreshold := now.Add(-controlPlaneRecentFailureWindow).Format(time.RFC3339)

	var err error
	if queueMetrics.QueuedOperations, err = operationsCol.CountDocuments(ctx, bson.M{"status": operations.StatusQueued}); err != nil {
		return platformmodels.ControlPlaneMetrics{}, err
	}
	if queueMetrics.DispatchingOperations, err = operationsCol.CountDocuments(ctx, bson.M{"dispatch.status": controlPlaneDispatchStatusSending}); err != nil {
		return platformmodels.ControlPlaneMetrics{}, err
	}
	if queueMetrics.DispatchFailedOperations, err = operationsCol.CountDocuments(ctx, bson.M{"dispatch.status": controlPlaneDispatchStatusFailed}); err != nil {
		return platformmodels.ControlPlaneMetrics{}, err
	}
	if queueMetrics.StaleQueuedOperations, err = operationsCol.CountDocuments(ctx, bson.M{
		"status":    operations.StatusQueued,
		"createdAt": bson.M{"$lte": staleThreshold},
	}); err != nil {
		return platformmodels.ControlPlaneMetrics{}, err
	}
	if queueMetrics.RecentDispatchFailures, err = operationsCol.CountDocuments(ctx, bson.M{
		"dispatch.lastErrorAt": bson.M{"$gte": recentFailureThreshold},
	}); err != nil {
		return platformmodels.ControlPlaneMetrics{}, err
	}

	var oldestQueued bson.M
	oldestQueuedErr := operationsCol.FindOne(
		ctx,
		bson.M{"status": operations.StatusQueued},
		options.FindOne().SetSort(bson.M{"createdAt": 1}),
	).Decode(&oldestQueued)
	if oldestQueuedErr == nil {
		queueMetrics.OldestQueuedAt = shared.StringValue(oldestQueued["createdAt"])
		if parsed, parseErr := time.Parse(time.RFC3339, queueMetrics.OldestQueuedAt); parseErr == nil {
			ageSeconds := int64(now.Sub(parsed.UTC()).Seconds())
			if ageSeconds > 0 {
				queueMetrics.OldestQueuedAgeSeconds = ageSeconds
			}
		}
	}

	var latestDispatchFailure bson.M
	latestDispatchFailureErr := operationsCol.FindOne(
		ctx,
		bson.M{"dispatch.lastErrorAt": bson.M{"$exists": true, "$ne": ""}},
		options.FindOne().SetSort(bson.M{"dispatch.lastErrorAt": -1}),
	).Decode(&latestDispatchFailure)
	if latestDispatchFailureErr == nil {
		dispatch := shared.MapPayload(latestDispatchFailure["dispatch"])
		queueMetrics.LastDispatchFailureAt = shared.StringValue(dispatch["lastErrorAt"])
	}

	queueMetrics.Status, queueMetrics.Summary = evaluateControlPlaneQueueStatus(queueMetrics)

	return platformmodels.ControlPlaneMetrics{
		Version: controlPlaneMetricsVersion,
		Queue:   queueMetrics,
	}, nil
}

func evaluateControlPlaneQueueStatus(metrics platformmodels.ControlPlaneQueueMetrics) (string, string) {
	switch {
	case metrics.DispatchFailedOperations > 0:
		return controlPlaneQueueStatusDegraded, "Dispatch failures need attention before the next platform change."
	case metrics.StaleQueuedOperations > 0:
		return controlPlaneQueueStatusDegraded, "Queued operations are aging without being claimed."
	case metrics.DispatchingOperations > 0:
		return controlPlaneQueueStatusReview, "Dispatch confirmation is currently in flight."
	case metrics.QueuedOperations > 0:
		return controlPlaneQueueStatusReview, "Operations are queued and waiting for worker execution."
	default:
		return controlPlaneQueueStatusHealthy, "Queue dispatch and operation handoff look healthy."
	}
}

func resolveQueueName() string {
	queueName := strings.TrimSpace(os.Getenv("WORKER_QUEUE"))
	if queueName == "" {
		return "releasea.worker"
	}
	return queueName
}

func resolveDeadLetterEnabled() bool {
	return shared.EnvBool("WORKER_QUEUE_DLQ_ENABLE", true)
}

func resolveDeadLetterQueueName(queueName string) string {
	if !resolveDeadLetterEnabled() {
		return ""
	}
	dlqName := strings.TrimSpace(os.Getenv("WORKER_QUEUE_DLQ_NAME"))
	if dlqName == "" {
		return queueName + defaultWorkerDeadLetterQueueNameSfx
	}
	return dlqName
}
