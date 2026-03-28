package rules

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var loadGovernanceSettingsForRulePublish = shared.LoadGovernanceSettings
var findRuleForPublishPolicyCheck = func(ctx context.Context, ruleID string) (bson.M, error) {
	return shared.FindOne(ctx, shared.Collection(shared.RulesCollection), bson.M{"id": ruleID})
}
var evaluateRulePublishPolicyCheck = func(
	ctx context.Context,
	environment string,
	internal bool,
	external bool,
) (rulePublishPolicyEvaluationResult, error) {
	settings, err := loadGovernanceSettingsForRulePublish(ctx)
	if err != nil {
		return rulePublishPolicyEvaluationResult{}, err
	}
	return rulePublishPolicyEvaluationResult{
		Environment: environment,
		Internal:    internal,
		External:    external,
		Violations:  shared.EvaluateExternalExposurePolicy(settings, environment, external),
	}, nil
}
var recordGovernanceRulePublishPolicyBlockAudit = func(
	ctx context.Context,
	ruleID string,
	ruleName string,
	performedBy bson.M,
	details bson.M,
) {
	auditID := "gaudit-" + uuid.NewString()
	doc := bson.M{
		"_id":          auditID,
		"id":           auditID,
		"action":       "governance.rule_publish_policy.blocked",
		"resourceType": "rule",
		"resourceId":   ruleID,
		"resourceName": ruleName,
		"performedBy":  performedBy,
		"performedAt":  shared.NowISO(),
		"details":      details,
	}
	_ = shared.InsertOne(ctx, shared.Collection(shared.GovernanceAuditCollection), doc)
}

type rulePublishPolicyEvaluationResult struct {
	Environment string                                   `json:"environment"`
	Internal    bool                                     `json:"internal"`
	External    bool                                     `json:"external"`
	Violations  []shared.GovernanceDeployPolicyViolation `json:"violations"`
}

func resolveRulePublishPolicyPerformedBy(c *gin.Context) bson.M {
	return bson.M{
		"id":    strings.TrimSpace(c.GetString("authUserId")),
		"name":  strings.TrimSpace(shared.AuthDisplayName(c)),
		"email": strings.TrimSpace(c.GetString("authEmail")),
	}
}

func maybeRespondRulePublishPolicyBlocked(
	c *gin.Context,
	ctx context.Context,
	settings bson.M,
	ruleID string,
	ruleName string,
	serviceID string,
	environment string,
	internal bool,
	external bool,
) bool {
	violations := shared.EvaluateExternalExposurePolicy(settings, environment, external)
	if len(violations) == 0 {
		return false
	}

	recordGovernanceRulePublishPolicyBlockAudit(ctx, ruleID, ruleName, resolveRulePublishPolicyPerformedBy(c), bson.M{
		"serviceId":    serviceID,
		"environment":  environment,
		"internal":     internal,
		"external":     external,
		"violations":   violations,
		"resourceType": "rule",
	})

	c.JSON(http.StatusConflict, gin.H{
		"queued":     false,
		"code":       "GOVERNANCE_EXPOSURE_POLICY_VIOLATION",
		"message":    violations[0].Message,
		"violations": violations,
	})
	return true
}

func GetRulePublishPolicyCheck(c *gin.Context) {
	ruleID := strings.TrimSpace(c.Param("id"))
	if ruleID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule ID required")
		return
	}

	environment := shared.NormalizeOperationEnvironment(c.Query("environment"))
	if environment == "" {
		shared.RespondError(c, http.StatusBadRequest, "Environment required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	if _, err := findRuleForPublishPolicyCheck(ctx, ruleID); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			shared.RespondError(c, http.StatusNotFound, "Rule not found")
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load rule")
		return
	}

	internal := shared.BoolValue(c.Query("internal"))
	external := shared.BoolValue(c.Query("external"))
	evaluation, err := evaluateRulePublishPolicyCheck(ctx, environment, internal, external)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to evaluate publish policy")
		return
	}
	c.JSON(http.StatusOK, evaluation)
}
