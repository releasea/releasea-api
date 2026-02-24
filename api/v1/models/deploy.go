package models

import (
	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type Deploy struct {
	ID          string
	ServiceID   string
	Status      string
	Environment string
	Commit      string
	Branch      string
	TriggeredBy string
	StartedAt   string
	FinishedAt  string
	Strategy    map[string]interface{}
}

func DeployFromBSON(doc bson.M) Deploy {
	return Deploy{
		ID:          shared.StringValue(doc["id"]),
		ServiceID:   shared.StringValue(doc["serviceId"]),
		Status:      shared.StringValue(doc["status"]),
		Environment: shared.StringValue(doc["environment"]),
		Commit:      shared.StringValue(doc["commit"]),
		Branch:      shared.StringValue(doc["branch"]),
		TriggeredBy: shared.StringValue(doc["triggeredBy"]),
		StartedAt:   shared.StringValue(doc["startedAt"]),
		FinishedAt:  shared.StringValue(doc["finishedAt"]),
		Strategy:    shared.MapPayload(doc["strategyStatus"]),
	}
}
