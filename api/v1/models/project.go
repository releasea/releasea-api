package models

import (
	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type Project struct {
	ID                   string
	SCMCredentialID      string
	RegistryCredentialID string
}

func ProjectFromBSON(doc bson.M) Project {
	return Project{
		ID:                   shared.StringValue(doc["id"]),
		SCMCredentialID:      shared.StringValue(doc["scmCredentialId"]),
		RegistryCredentialID: shared.StringValue(doc["registryCredentialId"]),
	}
}
