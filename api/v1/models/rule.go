package models

import (
	"releaseaapi/api/v1/shared"

	"go.mongodb.org/mongo-driver/bson"
)

type Rule struct {
	ID          string
	ServiceID   string
	Name        string
	Environment string
	Status      string
	Protocol    string
	Port        int
	Gateways    []string
	Hosts       []string
	Paths       []string
	Methods     []string
	Policy      map[string]interface{}
}

func RuleFromBSON(doc bson.M) Rule {
	return Rule{
		ID:          shared.StringValue(doc["id"]),
		ServiceID:   shared.StringValue(doc["serviceId"]),
		Name:        shared.StringValue(doc["name"]),
		Environment: shared.StringValue(doc["environment"]),
		Status:      shared.StringValue(doc["status"]),
		Protocol:    shared.StringValue(doc["protocol"]),
		Port:        shared.IntValue(doc["port"]),
		Gateways:    shared.ToStringSlice(doc["gateways"]),
		Hosts:       shared.ToStringSlice(doc["hosts"]),
		Paths:       shared.ToStringSlice(doc["paths"]),
		Methods:     shared.ToStringSlice(doc["methods"]),
		Policy:      shared.MapPayload(doc["policy"]),
	}
}
