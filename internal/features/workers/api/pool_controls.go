package workers

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type workerPoolMaintenancePayload struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

type workerPoolDrainPayload struct {
	Enabled bool   `json:"enabled"`
	Reason  string `json:"reason"`
}

type workerPoolControlDocument struct {
	ID                   string `json:"id"`
	MaintenanceEnabled   bool   `json:"maintenanceEnabled"`
	MaintenanceReason    string `json:"maintenanceReason,omitempty"`
	MaintenanceUpdatedAt string `json:"maintenanceUpdatedAt,omitempty"`
	MaintenanceUpdatedBy string `json:"maintenanceUpdatedBy,omitempty"`
	DrainEnabled         bool   `json:"drainEnabled"`
	DrainReason          string `json:"drainReason,omitempty"`
	DrainUpdatedAt       string `json:"drainUpdatedAt,omitempty"`
	DrainUpdatedBy       string `json:"drainUpdatedBy,omitempty"`
}

var findWorkerPoolControl = func(ctx context.Context, poolID string) (bson.M, error) {
	return shared.FindOne(ctx, shared.Collection(shared.WorkerPoolControlsCollection), bson.M{"_id": poolID})
}

var upsertWorkerPoolMaintenance = func(ctx context.Context, poolID string, payload workerPoolMaintenancePayload, updatedBy string) error {
	now := shared.NowISO()
	setFields := bson.M{
		"id":                   poolID,
		"poolId":               poolID,
		"maintenanceEnabled":   payload.Enabled,
		"reason":               strings.TrimSpace(payload.Reason),
		"maintenanceReason":    strings.TrimSpace(payload.Reason),
		"updatedAt":            now,
		"updatedBy":            strings.TrimSpace(updatedBy),
		"maintenanceUpdatedAt": now,
		"maintenanceUpdatedBy": strings.TrimSpace(updatedBy),
	}
	if payload.Enabled {
		setFields["drainEnabled"] = false
		setFields["drainReason"] = ""
		setFields["drainUpdatedAt"] = now
		setFields["drainUpdatedBy"] = strings.TrimSpace(updatedBy)
	}
	_, err := shared.Collection(shared.WorkerPoolControlsCollection).UpdateOne(
		ctx,
		bson.M{"_id": poolID},
		bson.M{
			"$set": setFields,
			"$setOnInsert": bson.M{
				"_id":       poolID,
				"createdAt": now,
			},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

var upsertWorkerPoolDrain = func(ctx context.Context, poolID string, payload workerPoolDrainPayload, updatedBy string) error {
	now := shared.NowISO()
	_, err := shared.Collection(shared.WorkerPoolControlsCollection).UpdateOne(
		ctx,
		bson.M{"_id": poolID},
		bson.M{
			"$set": bson.M{
				"id":             poolID,
				"poolId":         poolID,
				"drainEnabled":   payload.Enabled,
				"drainReason":    strings.TrimSpace(payload.Reason),
				"updatedAt":      now,
				"updatedBy":      strings.TrimSpace(updatedBy),
				"drainUpdatedAt": now,
				"drainUpdatedBy": strings.TrimSpace(updatedBy),
			},
			"$setOnInsert": bson.M{
				"_id":       poolID,
				"createdAt": now,
			},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

func mapWorkerPoolControlDocument(poolID string, control bson.M) workerPoolControlDocument {
	maintenanceReason := shared.StringValue(control["maintenanceReason"])
	if maintenanceReason == "" {
		maintenanceReason = shared.StringValue(control["reason"])
	}
	maintenanceUpdatedAt := shared.StringValue(control["maintenanceUpdatedAt"])
	if maintenanceUpdatedAt == "" {
		maintenanceUpdatedAt = shared.StringValue(control["updatedAt"])
	}
	maintenanceUpdatedBy := shared.StringValue(control["maintenanceUpdatedBy"])
	if maintenanceUpdatedBy == "" {
		maintenanceUpdatedBy = shared.StringValue(control["updatedBy"])
	}
	return workerPoolControlDocument{
		ID:                   poolID,
		MaintenanceEnabled:   shared.BoolValue(control["maintenanceEnabled"]),
		MaintenanceReason:    maintenanceReason,
		MaintenanceUpdatedAt: maintenanceUpdatedAt,
		MaintenanceUpdatedBy: maintenanceUpdatedBy,
		DrainEnabled:         shared.BoolValue(control["drainEnabled"]),
		DrainReason:          shared.StringValue(control["drainReason"]),
		DrainUpdatedAt:       shared.StringValue(control["drainUpdatedAt"]),
		DrainUpdatedBy:       shared.StringValue(control["drainUpdatedBy"]),
	}
}

func SetWorkerPoolMaintenance(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	poolID := strings.TrimSpace(c.Param("id"))
	if poolID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Pool ID required")
		return
	}

	var payload workerPoolMaintenancePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid maintenance payload")
		return
	}
	if payload.Enabled && strings.TrimSpace(payload.Reason) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Maintenance reason required when enabling maintenance mode")
		return
	}

	_, actorName, actorRole := shared.AuditActorFromContext(c)
	if err := upsertWorkerPoolMaintenance(ctx, poolID, payload, actorName); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update worker pool maintenance mode")
		return
	}

	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       map[bool]string{true: "worker_pool.maintenance.enabled", false: "worker_pool.maintenance.disabled"}[payload.Enabled],
		ResourceType: "worker_pool",
		ResourceID:   poolID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Message:      "Worker pool maintenance mode updated",
		Metadata: map[string]interface{}{
			"maintenanceEnabled": payload.Enabled,
			"reason":             strings.TrimSpace(payload.Reason),
		},
	})

	c.JSON(http.StatusOK, workerPoolControlDocument{
		ID:                   poolID,
		MaintenanceEnabled:   payload.Enabled,
		MaintenanceReason:    strings.TrimSpace(payload.Reason),
		MaintenanceUpdatedBy: actorName,
		MaintenanceUpdatedAt: shared.NowISO(),
	})
}

func SetWorkerPoolDrain(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	poolID := strings.TrimSpace(c.Param("id"))
	if poolID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Pool ID required")
		return
	}

	var payload workerPoolDrainPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid drain payload")
		return
	}
	if payload.Enabled && strings.TrimSpace(payload.Reason) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Drain reason required when enabling drain mode")
		return
	}

	if payload.Enabled {
		if control, err := findWorkerPoolControl(ctx, poolID); err == nil && shared.BoolValue(control["maintenanceEnabled"]) {
			shared.RespondError(c, http.StatusBadRequest, "Disable maintenance mode before enabling drain mode")
			return
		}
	}

	_, actorName, actorRole := shared.AuditActorFromContext(c)
	if err := upsertWorkerPoolDrain(ctx, poolID, payload, actorName); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update worker pool drain mode")
		return
	}

	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       map[bool]string{true: "worker_pool.drain.enabled", false: "worker_pool.drain.disabled"}[payload.Enabled],
		ResourceType: "worker_pool",
		ResourceID:   poolID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Message:      "Worker pool drain mode updated",
		Metadata: map[string]interface{}{
			"drainEnabled": payload.Enabled,
			"reason":       strings.TrimSpace(payload.Reason),
		},
	})

	c.JSON(http.StatusOK, workerPoolControlDocument{
		ID:             poolID,
		DrainEnabled:   payload.Enabled,
		DrainReason:    strings.TrimSpace(payload.Reason),
		DrainUpdatedBy: actorName,
		DrainUpdatedAt: shared.NowISO(),
	})
}

func GetCurrentWorkerPoolControl(c *gin.Context) {
	registrationValue, ok := c.Get("authWorkerRegistration")
	if !ok {
		shared.RespondError(c, http.StatusUnauthorized, "Worker registration required")
		return
	}
	registration, ok := registrationValue.(bson.M)
	if !ok {
		shared.RespondError(c, http.StatusUnauthorized, "Worker registration invalid")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	poolID := shared.WorkerPoolID(
		shared.StringValue(registration["environment"]),
		shared.StringValue(registration["cluster"]),
		shared.StringValue(registration["namespacePrefix"]),
		shared.ToStringSlice(registration["tags"]),
	)
	if poolID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Worker pool could not be resolved")
		return
	}

	control, err := findWorkerPoolControl(ctx, poolID)
	if err != nil {
		c.JSON(http.StatusOK, workerPoolControlDocument{ID: poolID})
		return
	}
	c.JSON(http.StatusOK, mapWorkerPoolControlDocument(poolID, control))
}
