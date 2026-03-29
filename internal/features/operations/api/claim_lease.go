package operations

import (
	"strings"
	"time"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	defaultOperationClaimLeaseTTLSeconds = 120
	minOperationClaimLeaseTTLSeconds     = 30
	maxOperationClaimLeaseTTLSeconds     = 600
)

type operationClaimStatusPayload struct {
	TTLSeconds int    `json:"ttlSeconds"`
	QueueName  string `json:"queueName"`
}

func normalizeOperationClaimLeaseTTL(value int) int {
	if value <= 0 {
		value = defaultOperationClaimLeaseTTLSeconds
	}
	if value < minOperationClaimLeaseTTLSeconds {
		value = minOperationClaimLeaseTTLSeconds
	}
	if value > maxOperationClaimLeaseTTLSeconds {
		value = maxOperationClaimLeaseTTLSeconds
	}
	return value
}

func buildOperationClaimMetadata(registration bson.M, regID string, claim *operationClaimStatusPayload, now time.Time) bson.M {
	now = now.UTC()
	nowISO := now.Format(time.RFC3339)
	ttlSeconds := normalizeOperationClaimLeaseTTL(0)
	queueName := ""
	if claim != nil {
		ttlSeconds = normalizeOperationClaimLeaseTTL(claim.TTLSeconds)
		queueName = strings.TrimSpace(claim.QueueName)
	}

	workerName := strings.TrimSpace(shared.StringValue(registration["name"]))
	if workerName == "" {
		workerName = strings.TrimSpace(shared.StringValue(registration["id"]))
	}
	if workerName == "" {
		workerName = "worker"
	}

	metadata := bson.M{
		"holder":               workerName,
		"workerRegistrationId": strings.TrimSpace(regID),
		"workerName":           workerName,
		"claimedAt":            nowISO,
		"lastHeartbeatAt":      nowISO,
		"leaseTTLSeconds":      ttlSeconds,
		"leaseExpiresAt":       now.Add(time.Duration(ttlSeconds) * time.Second).Format(time.RFC3339),
	}
	if queueName != "" {
		metadata["queueName"] = queueName
	}
	if workerID := strings.TrimSpace(shared.StringValue(registration["workerId"])); workerID != "" {
		metadata["workerId"] = workerID
	}
	return metadata
}
