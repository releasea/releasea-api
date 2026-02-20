package operations

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func GetOperations(c *gin.Context) {
	filter := bson.M{}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		filter["status"] = status
	}
	if resourceID := strings.TrimSpace(c.Query("resourceId")); resourceID != "" {
		filter["resourceId"] = resourceID
	}
	if opType := strings.TrimSpace(c.Query("type")); opType != "" {
		filter["type"] = opType
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.OperationsCollection), filter)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load operations")
		return
	}
	c.JSON(http.StatusOK, items)
}

func GetOperation(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		shared.RespondError(c, http.StatusBadRequest, "Operation ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	item, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{"id": id})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Operation not found")
		return
	}
	c.JSON(http.StatusOK, item)
}

func UpdateOperationStatus(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		shared.RespondError(c, http.StatusBadRequest, "Operation ID required")
		return
	}

	var payload struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Status == "" {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	regValue, ok := c.Get("authWorkerRegistration")
	if !ok {
		shared.RespondError(c, http.StatusForbidden, "Worker token required")
		return
	}
	registration, ok := regValue.(bson.M)
	if !ok {
		shared.RespondError(c, http.StatusForbidden, "Worker token invalid")
		return
	}
	regID := shared.StringValue(registration["id"])
	if regID == "" {
		regID = shared.StringValue(registration["_id"])
	}
	if regID == "" {
		shared.RespondError(c, http.StatusForbidden, "Worker registration missing")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	op, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{"id": id})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Operation not found")
		return
	}

	opRegID := shared.StringValue(op["workerRegistrationId"])
	if opRegID != "" && opRegID != regID {
		shared.RespondError(c, http.StatusForbidden, "Operation owned by another worker")
		return
	}

	currentStatus := shared.StringValue(op["status"])
	if payload.Status == StatusInProgress {
		if currentStatus != StatusQueued {
			shared.RespondError(c, http.StatusConflict, "Operation already started")
			return
		}
	}
	if payload.Status == StatusSucceeded || payload.Status == StatusFailed {
		if currentStatus == payload.Status {
			c.JSON(http.StatusOK, gin.H{"status": payload.Status})
			return
		}
		if currentStatus != StatusQueued && currentStatus != StatusInProgress {
			shared.RespondError(c, http.StatusConflict, "Operation already finished")
			return
		}
	}

	now := shared.NowISO()
	update := bson.M{
		"status":    payload.Status,
		"updatedAt": now,
	}
	if opRegID == "" {
		update["workerRegistrationId"] = regID
	}
	if payload.Status == StatusInProgress {
		update["startedAt"] = now
	}
	if payload.Status == StatusSucceeded || payload.Status == StatusFailed {
		update["finishedAt"] = now
	}
	if payload.Error != "" {
		update["error"] = payload.Error
	}
	var matched bool
	if payload.Status == StatusInProgress {
		filter := bson.M{"id": id, "status": StatusQueued}
		filter["$or"] = []bson.M{
			{"workerRegistrationId": bson.M{"$exists": false}},
			{"workerRegistrationId": ""},
			{"workerRegistrationId": regID},
		}
		result, err := shared.Collection(shared.OperationsCollection).UpdateOne(ctx, filter, bson.M{"$set": update})
		if err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to update operation")
			return
		}
		matched = result.MatchedCount > 0
	} else if payload.Status == StatusSucceeded || payload.Status == StatusFailed {
		filter := bson.M{
			"id": id,
			"status": bson.M{
				"$in": []string{StatusQueued, StatusInProgress},
			},
			"$or": []bson.M{
				{"workerRegistrationId": bson.M{"$exists": false}},
				{"workerRegistrationId": ""},
				{"workerRegistrationId": regID},
			},
		}
		result, err := shared.Collection(shared.OperationsCollection).UpdateOne(ctx, filter, bson.M{"$set": update})
		if err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to update operation")
			return
		}
		matched = result.MatchedCount > 0
	} else {
		if err := shared.UpdateByID(ctx, shared.Collection(shared.OperationsCollection), id, update); err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to update operation")
			return
		}
		matched = true
	}
	if !matched {
		shared.RespondError(c, http.StatusConflict, "Operation already processed")
		return
	}

	if payload.Status == StatusInProgress {
		applyOperationStart(ctx, op, now)
	}
	if payload.Status == StatusSucceeded {
		if err := applyOperationSuccess(ctx, op, now); err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to finalize operation")
			return
		}
	}
	if payload.Status == StatusFailed {
		if err := applyOperationFailure(ctx, op, now); err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to rollback operation")
			return
		}
	}

	workerName := shared.StringValue(registration["name"])
	if workerName == "" {
		workerName = shared.StringValue(registration["id"])
	}
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "operation." + payload.Status,
		ResourceType: "operation",
		ResourceID:   id,
		Status:       payload.Status,
		ActorID:      regID,
		ActorName:    workerName,
		ActorRole:    "worker",
		Source:       "worker",
		Message:      payload.Error,
		Metadata: map[string]interface{}{
			"type":         shared.StringValue(op["type"]),
			"resourceType": shared.StringValue(op["resourceType"]),
			"resourceId":   shared.StringValue(op["resourceId"]),
		},
	})
	notifyOperationResult(ctx, op, payload.Status, payload.Error)

	c.JSON(http.StatusOK, gin.H{"status": payload.Status})
}
