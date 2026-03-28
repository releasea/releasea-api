package rules

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestMaybeRespondRulePublishPolicyBlockedReturnsConflictJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Set("authUserId", "user-1")
	c.Set("authName", "Platform Admin")
	c.Set("authEmail", "admin@releasea.io")

	auditCalled := false
	originalAudit := recordGovernanceRulePublishPolicyBlockAudit
	recordGovernanceRulePublishPolicyBlockAudit = func(
		ctx context.Context,
		ruleID string,
		ruleName string,
		performedBy bson.M,
		details bson.M,
	) {
		auditCalled = true
	}
	defer func() {
		recordGovernanceRulePublishPolicyBlockAudit = originalAudit
	}()

	settings := bson.M{
		"deployPolicy": bson.M{
			"enabled": true,
			"rules": []interface{}{
				bson.M{
					"environment":           "prod",
					"blockExternalExposure": true,
				},
			},
		},
	}

	blocked := maybeRespondRulePublishPolicyBlocked(
		c,
		context.Background(),
		settings,
		"rule-1",
		"checkout",
		"svc-1",
		"prod",
		false,
		true,
	)
	if !blocked {
		t.Fatalf("expected publish policy helper to block the request")
	}
	if !auditCalled {
		t.Fatalf("expected governance audit to be recorded")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}

	var body struct {
		Code       string                                   `json:"code"`
		Message    string                                   `json:"message"`
		Violations []shared.GovernanceDeployPolicyViolation `json:"violations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("invalid json response: %v", err)
	}
	if body.Code != "GOVERNANCE_EXPOSURE_POLICY_VIOLATION" {
		t.Fatalf("code = %q, want %q", body.Code, "GOVERNANCE_EXPOSURE_POLICY_VIOLATION")
	}
	if len(body.Violations) != 1 {
		t.Fatalf("violations = %d, want %d", len(body.Violations), 1)
	}
}

func TestMaybeRespondRulePublishPolicyBlockedAllowsInternalOnlyPublish(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)

	settings := bson.M{
		"deployPolicy": bson.M{
			"enabled": true,
			"rules": []interface{}{
				bson.M{
					"environment":           "prod",
					"blockExternalExposure": true,
				},
			},
		},
	}

	blocked := maybeRespondRulePublishPolicyBlocked(
		c,
		context.Background(),
		settings,
		"rule-1",
		"checkout",
		"svc-1",
		"prod",
		true,
		false,
	)
	if blocked {
		t.Fatalf("expected internal-only publish to pass")
	}
}

func TestGetRulePublishPolicyCheckReturnsEvaluation(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindRule := findRuleForPublishPolicyCheck
	previousEvaluate := evaluateRulePublishPolicyCheck
	findRuleForPublishPolicyCheck = func(context.Context, string) (bson.M, error) {
		return bson.M{"id": "rule-1", "name": "checkout"}, nil
	}
	evaluateRulePublishPolicyCheck = func(context.Context, string, bool, bool) (rulePublishPolicyEvaluationResult, error) {
		return rulePublishPolicyEvaluationResult{
			Environment: "prod",
			Internal:    false,
			External:    true,
			Violations: []shared.GovernanceDeployPolicyViolation{
				{
					Code:        "external-exposure-disabled",
					Environment: "prod",
					Message:     "External exposure is blocked by policy for environment prod.",
				},
			},
		}, nil
	}
	defer func() {
		findRuleForPublishPolicyCheck = previousFindRule
		evaluateRulePublishPolicyCheck = previousEvaluate
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/rules/rule-1/publish-policy-check?environment=prod&external=true", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "rule-1"}}

	GetRulePublishPolicyCheck(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body struct {
		Environment string `json:"environment"`
		External    bool   `json:"external"`
		Violations  []struct {
			Code string `json:"code"`
		} `json:"violations"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid json: %v", err)
	}
	if body.Environment != "prod" || !body.External {
		t.Fatalf("unexpected evaluation payload: %s", recorder.Body.String())
	}
	if len(body.Violations) != 1 || body.Violations[0].Code != "external-exposure-disabled" {
		t.Fatalf("unexpected violations payload: %s", recorder.Body.String())
	}
}

func TestGetRulePublishPolicyCheckReturnsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFindRule := findRuleForPublishPolicyCheck
	findRuleForPublishPolicyCheck = func(context.Context, string) (bson.M, error) {
		return nil, mongo.ErrNoDocuments
	}
	defer func() {
		findRuleForPublishPolicyCheck = previousFindRule
	}()

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "/rules/rule-1/publish-policy-check?environment=prod", nil)
	ctx.Request = request
	ctx.Params = gin.Params{{Key: "id", Value: "rule-1"}}

	GetRulePublishPolicyCheck(ctx)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
