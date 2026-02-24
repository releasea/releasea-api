package models

import (
	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type Operation struct {
	ID           string
	Type         string
	Status       string
	ResourceID   string
	ResourceType string
	DeployID     string
	RuleDeployID string
	RequestedBy  string
	Payload      map[string]interface{}
	CreatedAt    string
	UpdatedAt    string
}

func OperationFromBSON(doc bson.M) Operation {
	return Operation{
		ID:           shared.StringValue(doc["id"]),
		Type:         shared.StringValue(doc["type"]),
		Status:       shared.StringValue(doc["status"]),
		ResourceID:   shared.StringValue(doc["resourceId"]),
		ResourceType: shared.StringValue(doc["resourceType"]),
		DeployID:     shared.StringValue(doc["deployId"]),
		RuleDeployID: shared.StringValue(doc["ruleDeployId"]),
		RequestedBy:  shared.StringValue(doc["requestedBy"]),
		Payload:      shared.MapPayload(doc["payload"]),
		CreatedAt:    shared.StringValue(doc["createdAt"]),
		UpdatedAt:    shared.StringValue(doc["updatedAt"]),
	}
}
