package shared

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	GovernanceApprovalTypeDeploy      = "deploy"
	GovernanceApprovalTypeRulePublish = "rule-publish"

	GovernanceApprovalStatusPending  = "pending"
	GovernanceApprovalStatusApproved = "approved"
	GovernanceApprovalStatusRejected = "rejected"
)

var governanceApprovalTypes = map[string]struct{}{
	GovernanceApprovalTypeDeploy:      {},
	GovernanceApprovalTypeRulePublish: {},
}

func NormalizeGovernanceApprovalType(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if _, ok := governanceApprovalTypes[normalized]; ok {
		return normalized
	}
	return ""
}

func NormalizeGovernanceApprovalStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case GovernanceApprovalStatusApproved:
		return GovernanceApprovalStatusApproved
	case GovernanceApprovalStatusRejected:
		return GovernanceApprovalStatusRejected
	case GovernanceApprovalStatusPending:
		return GovernanceApprovalStatusPending
	default:
		return ""
	}
}

func LoadGovernanceSettings(ctx context.Context) (bson.M, error) {
	settings, err := FindOne(ctx, Collection(GovernanceSettingsCollection), bson.M{})
	if err == nil {
		return settings, nil
	}
	if errors.Is(err, mongo.ErrNoDocuments) {
		return bson.M{
			"deployApproval": bson.M{
				"enabled":      false,
				"environments": []string{"prod"},
				"minApprovers": 1,
			},
			"rulePublishApproval": bson.M{
				"enabled":      false,
				"externalOnly": false,
				"minApprovers": 1,
			},
			"auditRetentionDays": 30,
		}, nil
	}
	return nil, err
}

func DeployApprovalRequired(settings bson.M, environment string) (bool, int) {
	config := MapPayload(settings["deployApproval"])
	if !BoolValue(config["enabled"]) {
		return false, 1
	}
	minApprovers := IntValue(config["minApprovers"])
	if minApprovers < 1 {
		minApprovers = 1
	}

	targetEnvironment := NormalizeOperationEnvironment(environment)
	allowedEnvironments := ToStringSlice(config["environments"])
	if len(allowedEnvironments) == 0 {
		return targetEnvironment == "prod", minApprovers
	}
	for _, value := range allowedEnvironments {
		if NormalizeOperationEnvironment(value) == targetEnvironment {
			return true, minApprovers
		}
	}
	return false, minApprovers
}

func RulePublishApprovalRequired(settings bson.M, external bool) (bool, int) {
	config := MapPayload(settings["rulePublishApproval"])
	if !BoolValue(config["enabled"]) {
		return false, 1
	}
	if BoolValue(config["externalOnly"]) && !external {
		return false, 1
	}
	minApprovers := IntValue(config["minApprovers"])
	if minApprovers < 1 {
		minApprovers = 1
	}
	return true, minApprovers
}

func MinApproversForApprovalType(settings bson.M, approvalType string) int {
	normalizedType := NormalizeGovernanceApprovalType(approvalType)
	minApprovers := 1
	switch normalizedType {
	case GovernanceApprovalTypeDeploy:
		minApprovers = IntValue(MapPayload(settings["deployApproval"])["minApprovers"])
	case GovernanceApprovalTypeRulePublish:
		minApprovers = IntValue(MapPayload(settings["rulePublishApproval"])["minApprovers"])
	}
	if minApprovers < 1 {
		return 1
	}
	return minApprovers
}

type GovernanceApprovalCreateParams struct {
	Type              string
	ResourceID        string
	ResourceName      string
	Environment       string
	RequestedBy       bson.M
	Metadata          map[string]interface{}
	RequiredApprovers int
}

func CreateOrGetPendingGovernanceApproval(ctx context.Context, params GovernanceApprovalCreateParams) (bson.M, bool, error) {
	approvalType := NormalizeGovernanceApprovalType(params.Type)
	if approvalType == "" {
		return nil, false, errors.New("unsupported approval type")
	}
	resourceID := strings.TrimSpace(params.ResourceID)
	if resourceID == "" {
		return nil, false, errors.New("resource ID required")
	}

	resourceName := strings.TrimSpace(params.ResourceName)
	if resourceName == "" {
		resourceName = resourceID
	}
	environment := strings.TrimSpace(params.Environment)
	if environment != "" {
		environment = NormalizeOperationEnvironment(environment)
	}

	filter := bson.M{
		"type":       approvalType,
		"resourceId": resourceID,
		"status":     GovernanceApprovalStatusPending,
	}
	if environment != "" {
		filter["environment"] = environment
	}
	existing, err := FindOne(ctx, Collection(GovernanceApprovalsCollection), filter)
	if err == nil {
		return existing, true, nil
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, err
	}

	requiredApprovers := params.RequiredApprovers
	if requiredApprovers < 1 {
		requiredApprovers = 1
	}

	requestedBy := bson.M{
		"id":    strings.TrimSpace(StringValue(params.RequestedBy["id"])),
		"name":  strings.TrimSpace(StringValue(params.RequestedBy["name"])),
		"email": strings.TrimSpace(StringValue(params.RequestedBy["email"])),
	}
	if StringValue(requestedBy["id"]) == "" {
		if name := StringValue(requestedBy["name"]); name != "" {
			requestedBy["id"] = ToKubeName(name)
		} else {
			requestedBy["id"] = "system"
		}
	}
	if StringValue(requestedBy["name"]) == "" {
		requestedBy["name"] = "System"
	}

	now := NowISO()
	approvalID := "apr-" + uuid.NewString()
	doc := bson.M{
		"_id":               approvalID,
		"id":                approvalID,
		"type":              approvalType,
		"status":            GovernanceApprovalStatusPending,
		"resourceId":        resourceID,
		"resourceName":      resourceName,
		"requestedBy":       requestedBy,
		"requestedAt":       now,
		"updatedAt":         now,
		"requiredApprovers": requiredApprovers,
		"approvalsCount":    0,
		"rejectionsCount":   0,
		"reviews":           []interface{}{},
	}
	if environment != "" {
		doc["environment"] = environment
	}
	if len(params.Metadata) > 0 {
		doc["metadata"] = params.Metadata
	}

	if err := InsertOne(ctx, Collection(GovernanceApprovalsCollection), doc); err != nil {
		return nil, false, err
	}
	return doc, false, nil
}
