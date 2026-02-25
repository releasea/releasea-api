package services

import (
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func ruleIDFromDoc(rule bson.M) string {
	if ruleID := shared.StringValue(rule["id"]); ruleID != "" {
		return ruleID
	}
	return shared.StringValue(rule["_id"])
}
