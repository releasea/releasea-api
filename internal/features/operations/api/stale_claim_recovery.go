package operations

import (
	"context"
	"net/http"
	"strings"
	"time"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	defaultStaleClaimRecoveryLimit     = 3
	defaultStaleClaimRecoveryBatchSize = 25
)

type staleClaimRecoveryMutation struct {
	Status      string
	AuditAction string
	Message     string
	Set         bson.M
	Unset       bson.M
}

func RecoverStaleOperationClaims(c *gin.Context) {
	if role, _ := c.Get("authRole"); role != "worker" {
		shared.RespondError(c, http.StatusForbidden, "Worker token required")
		return
	}

	registration := shared.MapPayload(c.MustGet("authWorkerRegistration"))
	recoveredBy := strings.TrimSpace(shared.StringValue(registration["name"]))
	if recoveredBy == "" {
		recoveredBy = strings.TrimSpace(shared.StringValue(registration["id"]))
	}
	if recoveredBy == "" {
		recoveredBy = "worker"
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()

	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)
	cursor, err := shared.Collection(shared.OperationsCollection).Find(
		ctx,
		bson.M{
			"status":               StatusInProgress,
			"claim.leaseExpiresAt": bson.M{"$lte": nowISO},
		},
		options.Find().SetSort(bson.D{
			{Key: "claim.leaseExpiresAt", Value: 1},
			{Key: "updatedAt", Value: 1},
		}).SetLimit(defaultStaleClaimRecoveryBatchSize),
	)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load stale operation claims")
		return
	}
	defer cursor.Close(ctx)

	recovered := 0
	failed := 0
	scanned := 0
	for cursor.Next(ctx) {
		var op bson.M
		if err := cursor.Decode(&op); err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to decode stale operation claim")
			return
		}
		scanned++

		id := strings.TrimSpace(shared.StringValue(op["id"]))
		claim := shared.MapPayload(op["claim"])
		leaseExpiresAt := strings.TrimSpace(shared.StringValue(claim["leaseExpiresAt"]))
		if id == "" || leaseExpiresAt == "" {
			continue
		}

		mutation := buildStaleClaimRecoveryMutation(op, recoveredBy, now)
		updateDoc := bson.M{"$set": mutation.Set}
		if len(mutation.Unset) > 0 {
			updateDoc["$unset"] = mutation.Unset
		}

		result, err := shared.Collection(shared.OperationsCollection).UpdateOne(ctx, bson.M{
			"id":                   id,
			"status":               StatusInProgress,
			"claim.leaseExpiresAt": leaseExpiresAt,
		}, updateDoc)
		if err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to recover stale operation claim")
			return
		}
		if result.MatchedCount == 0 {
			continue
		}

		if mutation.Status == StatusQueued {
			recovered++
		} else if mutation.Status == StatusFailed {
			failed++
			notifyOperationResult(ctx, op, StatusFailed, mutation.Message)
		}

		shared.RecordAuditEvent(ctx, shared.AuditEvent{
			Action:       mutation.AuditAction,
			ResourceType: "operation",
			ResourceID:   id,
			Status:       mutation.Status,
			ActorID:      recoveredBy,
			ActorName:    recoveredBy,
			ActorRole:    "worker",
			Source:       "worker",
			Message:      mutation.Message,
			Metadata: map[string]interface{}{
				"type":                 shared.StringValue(op["type"]),
				"resourceType":         shared.StringValue(op["resourceType"]),
				"resourceId":           shared.StringValue(op["resourceId"]),
				"lastLeaseExpiresAt":   leaseExpiresAt,
				"recoveryCount":        mutation.Set["recovery.count"],
				"lastRecoveryDecision": mutation.Set["recovery.lastDisposition"],
			},
		})
	}
	if err := cursor.Err(); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to iterate stale operation claims")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"recovered": recovered,
		"failed":    failed,
		"scanned":   scanned,
	})
}

func buildStaleClaimRecoveryMutation(op bson.M, recoveredBy string, now time.Time) staleClaimRecoveryMutation {
	now = now.UTC()
	nowISO := now.Format(time.RFC3339)
	claim := shared.MapPayload(op["claim"])
	recovery := shared.MapPayload(op["recovery"])
	recoveryCount := shared.IntValue(recovery["count"]) + 1
	leaseExpiresAt := strings.TrimSpace(shared.StringValue(claim["leaseExpiresAt"]))
	holder := strings.TrimSpace(shared.StringValue(claim["holder"]))
	workerRegistrationID := strings.TrimSpace(shared.StringValue(claim["workerRegistrationId"]))

	setDoc := bson.M{
		"updatedAt":                         nowISO,
		"workerRegistrationId":              "",
		"claim.lastWorkerStatusAt":          nowISO,
		"claim.releasedAt":                  nowISO,
		"recovery.count":                    recoveryCount,
		"recovery.lastRecoveredAt":          nowISO,
		"recovery.lastRecoveredBy":          strings.TrimSpace(recoveredBy),
		"recovery.lastLeaseExpiresAt":       leaseExpiresAt,
		"recovery.lastHolder":               holder,
		"recovery.lastWorkerRegistrationId": workerRegistrationID,
	}
	unsetDoc := bson.M{
		"startedAt":  "",
		"finishedAt": "",
	}

	if recoveryCount >= defaultStaleClaimRecoveryLimit {
		message := "operation claim lease expired too many times"
		setDoc["status"] = StatusFailed
		setDoc["finishedAt"] = nowISO
		setDoc["error"] = message
		setDoc["claim.lastWorkerStatus"] = StatusFailed
		setDoc["recovery.lastDisposition"] = "failed"
		setDoc["recovery.lastReason"] = message
		delete(unsetDoc, "finishedAt")
		return staleClaimRecoveryMutation{
			Status:      StatusFailed,
			AuditAction: "operation.claim.exhausted",
			Message:     message,
			Set:         setDoc,
			Unset:       unsetDoc,
		}
	}

	message := "operation claim lease expired and was requeued"
	setDoc["status"] = StatusQueued
	setDoc["claim.lastWorkerStatus"] = StatusQueued
	setDoc["recovery.lastDisposition"] = "requeued"
	setDoc["recovery.lastReason"] = message
	return staleClaimRecoveryMutation{
		Status:      StatusQueued,
		AuditAction: "operation.claim.recovered",
		Message:     message,
		Set:         setDoc,
		Unset:       unsetDoc,
	}
}
