package governance

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"

	governancemodels "releaseaapi/internal/features/governance/models"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	approvalStatusPending  = shared.GovernanceApprovalStatusPending
	approvalStatusApproved = shared.GovernanceApprovalStatusApproved
	approvalStatusRejected = shared.GovernanceApprovalStatusRejected
)

var findGovernanceAuditEntries = func(ctx context.Context) ([]bson.M, error) {
	return shared.FindAll(ctx, shared.Collection(shared.GovernanceAuditCollection), bson.M{})
}

var findPlatformAuditEntries = func(ctx context.Context) ([]bson.M, error) {
	return shared.FindAll(ctx, shared.Collection(shared.PlatformAuditCollection), bson.M{})
}

func authValue(c *gin.Context, key string) string {
	if value, ok := c.Get(key); ok {
		if text, ok := value.(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

func isAdminRequest(c *gin.Context) bool {
	return strings.EqualFold(authValue(c, "authRole"), "admin")
}

func requireAdminRequest(c *gin.Context) bool {
	if isAdminRequest(c) {
		return true
	}
	shared.RespondError(c, http.StatusForbidden, "Admin role required")
	return false
}

func normalizeApprovalStatus(value string) string {
	return shared.NormalizeGovernanceApprovalStatus(value)
}

func normalizeApprovalType(value string) string {
	return shared.NormalizeGovernanceApprovalType(value)
}

func normalizeGovernanceEnvironments(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		normalized := shared.NormalizeOperationEnvironment(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	if len(result) == 0 {
		return []string{"prod"}
	}
	sort.Strings(result)
	return result
}

func normalizeDeployPolicyRules(values []governancemodels.DeployPolicyRule) []bson.M {
	raw := make([]interface{}, 0, len(values))
	for _, value := range values {
		raw = append(raw, bson.M{
			"environment":              value.Environment,
			"allowAutoDeploy":          value.AllowAutoDeploy,
			"requireExplicitVersion":   value.RequireExplicitVersion,
			"blockExternalExposure":    value.BlockExternalExposure,
			"allowedProfileIds":        value.AllowedProfileIDs,
			"allowedScmProviders":      value.AllowedSCMProviders,
			"allowedRegistryProviders": value.AllowedRegistryProviders,
			"allowedSecretProviders":   value.AllowedSecretProviders,
			"allowedSourceTypes":       value.AllowedSourceTypes,
			"allowedRegistries":        value.AllowedRegistries,
			"allowedStrategies":        value.AllowedStrategies,
			"maxReplicas":              value.MaxReplicas,
		})
	}
	return shared.NormalizeDeployPolicyRules(raw)
}

func recordGovernanceAudit(
	ctx context.Context,
	action string,
	resourceType string,
	resourceID string,
	resourceName string,
	performedBy bson.M,
	details map[string]interface{},
) {
	now := shared.NowISO()
	auditID := "gaudit-" + uuid.NewString()
	doc := bson.M{
		"_id":          auditID,
		"id":           auditID,
		"action":       action,
		"resourceType": resourceType,
		"resourceId":   resourceID,
		"resourceName": resourceName,
		"performedBy":  performedBy,
		"performedAt":  now,
	}
	if len(details) > 0 {
		doc["details"] = details
	}
	_ = shared.InsertOne(ctx, shared.Collection(shared.GovernanceAuditCollection), doc)
}

func normalizeGovernanceAuditEntry(item bson.M) bson.M {
	performedBy := shared.MapPayload(item["performedBy"])
	if len(performedBy) == 0 {
		performedBy = map[string]interface{}{
			"id":    "system",
			"name":  "System",
			"email": "",
		}
	}

	resourceID := shared.StringValue(item["resourceId"])
	resourceName := strings.TrimSpace(shared.StringValue(item["resourceName"]))
	if resourceName == "" {
		resourceName = resourceID
	}
	if resourceName == "" {
		resourceName = "Governance event"
	}

	entry := bson.M{
		"id":           shared.StringValue(item["id"]),
		"action":       shared.StringValue(item["action"]),
		"resourceType": shared.StringValue(item["resourceType"]),
		"resourceId":   resourceID,
		"resourceName": resourceName,
		"performedBy":  performedBy,
		"performedAt":  shared.StringValue(item["performedAt"]),
	}
	if details := shared.MapPayload(item["details"]); len(details) > 0 {
		entry["details"] = details
	}
	if ipAddress := strings.TrimSpace(shared.StringValue(item["ipAddress"])); ipAddress != "" {
		entry["ipAddress"] = ipAddress
	}
	return entry
}

func resolvePlatformAuditResourceName(item bson.M, actor map[string]interface{}, metadata map[string]interface{}) string {
	if name := strings.TrimSpace(shared.StringValue(metadata["name"])); name != "" {
		return name
	}
	if name := strings.TrimSpace(shared.StringValue(metadata["resourceName"])); name != "" {
		return name
	}

	resourceType := strings.TrimSpace(shared.StringValue(item["resourceType"]))
	resourceID := strings.TrimSpace(shared.StringValue(item["resourceId"]))

	if resourceType == "user" {
		if name := strings.TrimSpace(shared.StringValue(actor["name"])); name != "" {
			return name
		}
	}

	if resourceType == "operation" {
		nestedType := strings.TrimSpace(shared.StringValue(metadata["resourceType"]))
		nestedID := strings.TrimSpace(shared.StringValue(metadata["resourceId"]))
		if nestedType != "" && nestedID != "" {
			return fmt.Sprintf("%s %s", nestedType, nestedID)
		}
		if operationType := strings.TrimSpace(shared.StringValue(metadata["type"])); operationType != "" {
			if resourceID != "" {
				return fmt.Sprintf("%s (%s)", operationType, resourceID)
			}
			return operationType
		}
	}

	if resourceID != "" {
		return resourceID
	}
	if resourceType != "" {
		return fmt.Sprintf("%s event", resourceType)
	}
	return "Platform event"
}

func normalizePlatformAuditEntry(item bson.M) bson.M {
	actor := shared.MapPayload(item["actor"])
	if len(actor) == 0 {
		actor = map[string]interface{}{
			"id":   "system",
			"name": "System",
			"role": "",
		}
	}
	metadata := shared.MapPayload(item["metadata"])
	details := make(map[string]interface{}, len(metadata)+3)
	for key, value := range metadata {
		details[key] = value
	}
	if source := strings.TrimSpace(shared.StringValue(item["source"])); source != "" {
		details["source"] = source
	}
	if status := strings.TrimSpace(shared.StringValue(item["status"])); status != "" {
		details["status"] = status
	}
	if message := strings.TrimSpace(shared.StringValue(item["message"])); message != "" {
		details["message"] = message
	}

	resourceID := strings.TrimSpace(shared.StringValue(item["resourceId"]))
	entry := bson.M{
		"id":           shared.StringValue(item["id"]),
		"action":       shared.StringValue(item["action"]),
		"resourceType": shared.StringValue(item["resourceType"]),
		"resourceId":   resourceID,
		"resourceName": resolvePlatformAuditResourceName(item, actor, metadata),
		"performedBy": bson.M{
			"id":    shared.StringValue(actor["id"]),
			"name":  shared.StringValue(actor["name"]),
			"email": "",
		},
		"performedAt": shared.StringValue(item["createdAt"]),
	}
	if len(details) > 0 {
		entry["details"] = details
	}
	return entry
}

func buildUnifiedGovernanceAuditFeed(platformEntries, governanceEntries []bson.M) []bson.M {
	items := make([]bson.M, 0, len(platformEntries)+len(governanceEntries))
	for _, entry := range platformEntries {
		items = append(items, normalizePlatformAuditEntry(entry))
	}
	for _, entry := range governanceEntries {
		items = append(items, normalizeGovernanceAuditEntry(entry))
	}
	sort.Slice(items, func(i, j int) bool {
		return shared.StringValue(items[i]["performedAt"]) > shared.StringValue(items[j]["performedAt"])
	})
	return items
}

func GetGovernanceSettings(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	settings, err := shared.FindOne(ctx, shared.Collection(shared.GovernanceSettingsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Governance settings not found")
		return
	}
	c.JSON(http.StatusOK, settings)
}

func UpdateGovernanceSettings(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}
	var payload governancemodels.GovernanceSettingsPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	settings, err := shared.FindOne(ctx, shared.Collection(shared.GovernanceSettingsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Governance settings not found")
		return
	}
	id, _ := settings["_id"].(string)
	if id == "" {
		shared.RespondError(c, http.StatusNotFound, "Governance settings not found")
		return
	}
	minDeployApprovers := payload.DeployApproval.MinApprovers
	if minDeployApprovers < 1 {
		minDeployApprovers = 1
	}
	minRuleApprovers := payload.RulePublishApproval.MinApprovers
	if minRuleApprovers < 1 {
		minRuleApprovers = 1
	}
	auditRetentionDays := payload.AuditRetentionDays
	if auditRetentionDays < 30 {
		auditRetentionDays = 30
	}

	update := bson.M{
		"deployApproval": bson.M{
			"enabled":      payload.DeployApproval.Enabled,
			"environments": normalizeGovernanceEnvironments(payload.DeployApproval.Environments),
			"minApprovers": minDeployApprovers,
		},
		"deployPolicy": bson.M{
			"enabled": payload.DeployPolicy.Enabled,
			"rules":   normalizeDeployPolicyRules(payload.DeployPolicy.Rules),
		},
		"rulePublishApproval": bson.M{
			"enabled":      payload.RulePublishApproval.Enabled,
			"externalOnly": payload.RulePublishApproval.ExternalOnly,
			"minApprovers": minRuleApprovers,
		},
		"auditRetentionDays": auditRetentionDays,
		"updatedAt":          shared.NowISO(),
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.GovernanceSettingsCollection), id, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update governance settings")
		return
	}

	performedBy := bson.M{
		"id":    authValue(c, "authUserId"),
		"name":  authValue(c, "authName"),
		"email": authValue(c, "authEmail"),
	}
	recordGovernanceAudit(
		ctx,
		"governance.settings.updated",
		"settings",
		id,
		"Governance Settings",
		performedBy,
		map[string]interface{}{
			"deployApproval":      update["deployApproval"],
			"deployPolicy":        update["deployPolicy"],
			"rulePublishApproval": update["rulePublishApproval"],
			"auditRetentionDays":  auditRetentionDays,
		},
	)

	updated, _ := shared.FindOne(ctx, shared.Collection(shared.GovernanceSettingsCollection), bson.M{"_id": id})
	c.JSON(http.StatusOK, updated)
}

func GetGovernanceApprovals(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	filter := bson.M{}
	if !isAdminRequest(c) {
		userID := authValue(c, "authUserId")
		if userID == "" {
			shared.RespondError(c, http.StatusForbidden, "User context required")
			return
		}
		filter["requestedBy.id"] = userID
	}

	items, err := shared.FindAll(ctx, shared.Collection(shared.GovernanceApprovalsCollection), filter)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load approvals")
		return
	}
	sort.Slice(items, func(i, j int) bool {
		return shared.StringValue(items[i]["requestedAt"]) > shared.StringValue(items[j]["requestedAt"])
	})
	c.JSON(http.StatusOK, items)
}

func CreateGovernanceApproval(c *gin.Context) {
	var payload governancemodels.CreateApprovalPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	approvalType := normalizeApprovalType(payload.Type)
	if approvalType == "" {
		shared.RespondError(c, http.StatusBadRequest, "Unsupported approval type")
		return
	}
	resourceID := strings.TrimSpace(payload.ResourceID)
	if resourceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Resource ID required")
		return
	}
	resourceName := strings.TrimSpace(payload.ResourceName)
	if resourceName == "" {
		resourceName = resourceID
	}
	environment := strings.TrimSpace(payload.Environment)
	if environment != "" {
		environment = shared.NormalizeOperationEnvironment(environment)
	}

	requestedBy := bson.M{
		"id":    authValue(c, "authUserId"),
		"name":  authValue(c, "authName"),
		"email": authValue(c, "authEmail"),
	}
	if shared.StringValue(requestedBy["id"]) == "" {
		shared.RespondError(c, http.StatusForbidden, "User context required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	settings, err := shared.LoadGovernanceSettings(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load governance settings")
		return
	}
	requiredApprovers := shared.MinApproversForApprovalType(settings, approvalType)
	doc, _, err := shared.CreateOrGetPendingGovernanceApproval(ctx, shared.GovernanceApprovalCreateParams{
		Type:              approvalType,
		ResourceID:        resourceID,
		ResourceName:      resourceName,
		Environment:       environment,
		RequestedBy:       requestedBy,
		Metadata:          payload.Metadata,
		RequiredApprovers: requiredApprovers,
	})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create approval")
		return
	}

	approvalID := shared.StringValue(doc["id"])

	recordGovernanceAudit(
		ctx,
		"governance.approval.requested",
		"approval",
		approvalID,
		resourceName,
		requestedBy,
		map[string]interface{}{
			"type":         approvalType,
			"resourceId":   resourceID,
			"environment":  environment,
			"minApprovers": requiredApprovers,
		},
	)

	c.JSON(http.StatusOK, doc)
}

func ReviewGovernanceApproval(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}
	approvalID := c.Param("id")
	if strings.TrimSpace(approvalID) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Approval ID required")
		return
	}
	var payload governancemodels.ReviewApprovalPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	status := normalizeApprovalStatus(payload.Status)
	if status == "" || status == approvalStatusPending {
		shared.RespondError(c, http.StatusBadRequest, "Invalid approval status")
		return
	}
	reviewer := bson.M{
		"id":    authValue(c, "authUserId"),
		"name":  authValue(c, "authName"),
		"email": authValue(c, "authEmail"),
	}
	reviewerID := strings.TrimSpace(shared.StringValue(reviewer["id"]))
	if reviewerID == "" {
		shared.RespondError(c, http.StatusForbidden, "Reviewer context required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	approval, err := shared.FindOne(ctx, shared.Collection(shared.GovernanceApprovalsCollection), bson.M{"id": approvalID})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			shared.RespondError(c, http.StatusNotFound, "Approval not found")
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load approval")
		return
	}
	if strings.TrimSpace(shared.StringValue(approval["status"])) != approvalStatusPending {
		shared.RespondError(c, http.StatusConflict, "Approval is already reviewed")
		return
	}

	reviews := extractApprovalReviews(approval["reviews"])
	if hasReviewerReview(reviews, reviewerID) {
		shared.RespondError(c, http.StatusConflict, "Reviewer has already submitted a review")
		return
	}

	now := shared.NowISO()
	reviewEntry := bson.M{
		"status":     status,
		"reviewedAt": now,
		"reviewedBy": reviewer,
		"comment":    strings.TrimSpace(payload.Comment),
	}
	reviews = append(reviews, reviewEntry)

	approvedCount := countReviewsByStatus(reviews, approvalStatusApproved)
	rejectedCount := countReviewsByStatus(reviews, approvalStatusRejected)
	requiredApprovers := resolveRequiredApprovers(ctx, approval)
	finalStatus := approvalStatusPending
	if rejectedCount > 0 {
		finalStatus = approvalStatusRejected
	} else if approvedCount >= requiredApprovers {
		finalStatus = approvalStatusApproved
	}
	remainingApprovals := requiredApprovers - approvedCount
	if remainingApprovals < 0 {
		remainingApprovals = 0
	}

	update := bson.M{
		"reviews":           reviews,
		"approvalsCount":    approvedCount,
		"rejectionsCount":   rejectedCount,
		"requiredApprovers": requiredApprovers,
		"lastReviewedAt":    now,
		"lastReviewedBy":    reviewer,
		"updatedAt":         now,
	}
	execution := bson.M{}
	if finalStatus != approvalStatusPending {
		update["status"] = finalStatus
		update["reviewedAt"] = now
		update["reviewedBy"] = reviewer
		update["reviewComment"] = strings.TrimSpace(payload.Comment)
		if finalStatus == approvalStatusApproved {
			execution, err = executeApprovedAction(ctx, mergeApprovalWithReviewState(approval, reviews, approvedCount, rejectedCount, requiredApprovers))
			if err != nil {
				execution = bson.M{
					"status":    "failed",
					"error":     err.Error(),
					"updatedAt": now,
				}
			}
			update["execution"] = execution
		}
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.GovernanceApprovalsCollection), approvalID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update approval")
		return
	}

	recordGovernanceAudit(
		ctx,
		"governance.approval.reviewed",
		"approval",
		approvalID,
		shared.StringValue(approval["resourceName"]),
		reviewer,
		map[string]interface{}{
			"status":             finalStatus,
			"decision":           status,
			"comment":            strings.TrimSpace(payload.Comment),
			"approvedCount":      approvedCount,
			"rejectedCount":      rejectedCount,
			"requiredApprovers":  requiredApprovers,
			"remainingApprovals": remainingApprovals,
		},
	)

	response := gin.H{
		"success":            true,
		"status":             finalStatus,
		"approvedCount":      approvedCount,
		"rejectedCount":      rejectedCount,
		"requiredApprovers":  requiredApprovers,
		"remainingApprovals": remainingApprovals,
	}
	if len(execution) > 0 {
		response["execution"] = execution
	}
	if finalStatus == approvalStatusPending {
		response["message"] = fmt.Sprintf("Approval recorded. %d more approval(s) required.", remainingApprovals)
	}
	c.JSON(http.StatusOK, response)
}

func DeleteGovernanceApproval(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}
	approvalID := c.Param("id")
	if strings.TrimSpace(approvalID) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Approval ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	approval, _ := shared.FindOne(ctx, shared.Collection(shared.GovernanceApprovalsCollection), bson.M{"id": approvalID})
	if err := shared.DeleteByID(ctx, shared.Collection(shared.GovernanceApprovalsCollection), approvalID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete approval")
		return
	}
	recordGovernanceAudit(
		ctx,
		"governance.approval.deleted",
		"approval",
		approvalID,
		shared.StringValue(approval["resourceName"]),
		bson.M{
			"id":    authValue(c, "authUserId"),
			"name":  authValue(c, "authName"),
			"email": authValue(c, "authEmail"),
		},
		nil,
	)
	c.Status(http.StatusNoContent)
}

func GetGovernanceAudit(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	governanceEntries, err := findGovernanceAuditEntries(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load audit logs")
		return
	}
	platformEntries, err := findPlatformAuditEntries(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load audit logs")
		return
	}
	c.JSON(http.StatusOK, buildUnifiedGovernanceAuditFeed(platformEntries, governanceEntries))
}
