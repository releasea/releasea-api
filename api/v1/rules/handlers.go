package rules

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"releaseaapi/api/v1/operations"
	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func GetRules(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.RulesCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load rules")
		return
	}
	c.JSON(http.StatusOK, items)
}

func GetRule(c *gin.Context) {
	ruleID := c.Param("id")
	if ruleID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	item, err := shared.FindOne(ctx, shared.Collection(shared.RulesCollection), bson.M{"id": ruleID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Rule not found")
		return
	}
	c.JSON(http.StatusOK, item)
}

func AppendRuleLogs(c *gin.Context) {
	ruleID := c.Param("id")
	if ruleID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule ID required")
		return
	}
	var payload struct {
		Lines []string `json:"lines"`
		Line  string   `json:"line"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	lines := payload.Lines
	if payload.Line != "" {
		lines = append(lines, payload.Line)
	}
	if len(lines) == 0 {
		shared.RespondError(c, http.StatusBadRequest, "No log lines provided")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	update := bson.M{
		"$push": bson.M{
			"logs": bson.M{
				"$each": lines,
			},
		},
		"$set": bson.M{
			"updatedAt": shared.NowISO(),
		},
	}
	col := shared.Collection(shared.RulesCollection)
	if _, err := col.UpdateOne(ctx, bson.M{"_id": ruleID}, update); err != nil {
		log.Printf("[db] error during appendRuleLogs on %s: %v", col.Name(), err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to append rule logs")
		return
	}
	c.Status(http.StatusNoContent)
}

func CreateRule(c *gin.Context) {
	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	createRuleFromPayload(c, payload, "")
}

func CreateServiceRule(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}
	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	createRuleFromPayload(c, payload, serviceID)
}

func createRuleFromPayload(c *gin.Context, payload map[string]interface{}, serviceIDOverride string) {
	serviceID := strings.TrimSpace(serviceIDOverride)
	if serviceID == "" {
		serviceID = shared.StringValue(payload["serviceId"])
	}
	name := strings.TrimSpace(shared.StringValue(payload["name"]))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "serviceId is required")
		return
	}
	if name == "" {
		name = fmt.Sprintf("%s-rule", serviceID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}
	serviceName := shared.StringValue(service["name"])
	name = shared.CanonicalRuleName(name, serviceName)

	environment := shared.StringValue(payload["environment"])
	if environment == "" {
		environment = "prod"
	}
	port := shared.IntValue(payload["port"])
	if port <= 0 {
		port = shared.IntValue(service["port"])
	}
	if port <= 0 {
		port = 80
	}

	paths := shared.ToStringSlice(payload["paths"])
	if len(paths) == 0 {
		paths = []string{"/"}
	}
	methods := shared.ToStringSlice(payload["methods"])
	if len(methods) == 0 {
		methods = []string{"GET"}
	}
	hosts := shared.ToStringSlice(payload["hosts"])

	policyMap := shared.MapPayload(payload["policy"])
	action := shared.StringValue(policyMap["action"])
	if action == "" {
		action = "allow"
	}
	policy := bson.M{
		"action":    action,
		"timeoutMs": shared.IntValue(policyMap["timeoutMs"]),
		"retries":   shared.IntValue(policyMap["retries"]),
		"ipPolicy":  shared.StringValue(policyMap["ipPolicy"]),
	}
	if shared.IntValue(policy["timeoutMs"]) <= 0 {
		policy["timeoutMs"] = 1500
	}
	if shared.IntValue(policy["retries"]) <= 0 {
		policy["retries"] = 2
	}
	if shared.StringValue(policy["ipPolicy"]) == "" {
		policy["ipPolicy"] = "open"
	}

	internal := shared.BoolValue(payload["internal"])
	external := shared.BoolValue(payload["external"])
	wantsPublish := internal || external
	requiresApproval := false
	requiredApprovers := 1
	if wantsPublish {
		settings, settingsErr := shared.LoadGovernanceSettings(ctx)
		if settingsErr != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to load governance settings")
			return
		}
		requiresApproval, requiredApprovers = shared.RulePublishApprovalRequired(settings, external)
	}

	gateways := []string{}
	status := "draft"
	if wantsPublish && !requiresApproval {
		gateways = operations.BuildGateways(nil, internal, external, environment)
		status = operations.StatusQueued
	}

	now := shared.NowISO()
	ruleID := "rule-" + uuid.NewString()
	ruleDoc := bson.M{
		"_id":         ruleID,
		"id":          ruleID,
		"name":        name,
		"serviceId":   serviceID,
		"environment": environment,
		"hosts":       hosts,
		"gateways":    gateways,
		"paths":       paths,
		"methods":     methods,
		"protocol":    shared.StringValue(payload["protocol"]),
		"port":        port,
		"status":      status,
		"createdAt":   now,
		"updatedAt":   now,
		"policy":      policy,
		"logs":        []interface{}{},
	}
	if shared.StringValue(ruleDoc["protocol"]) == "" {
		ruleDoc["protocol"] = "https"
	}

	if err := shared.InsertOne(ctx, shared.Collection(shared.RulesCollection), ruleDoc); err != nil {
		log.Printf("[rule.create] failed to insert rule: %v", err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create rule")
		return
	}

	if wantsPublish {
		if requiresApproval {
			approvalDoc, _, approvalErr := shared.CreateOrGetPendingGovernanceApproval(ctx, shared.GovernanceApprovalCreateParams{
				Type:         shared.GovernanceApprovalTypeRulePublish,
				ResourceID:   ruleID,
				ResourceName: name,
				Environment:  environment,
				RequestedBy: bson.M{
					"id":    strings.TrimSpace(c.GetString("authUserId")),
					"name":  shared.AuthDisplayName(c),
					"email": strings.TrimSpace(c.GetString("authEmail")),
				},
				Metadata: map[string]interface{}{
					"environment": environment,
					"serviceId":   serviceID,
					"hosts":       hosts,
					"gateways":    operations.BuildGateways(nil, internal, external, environment),
					"action": map[string]interface{}{
						"kind":        "rule.publish",
						"ruleId":      ruleID,
						"serviceId":   serviceID,
						"environment": environment,
						"internal":    internal,
						"external":    external,
						"requestedBy": shared.AuthDisplayName(c),
					},
				},
				RequiredApprovers: requiredApprovers,
			})
			if approvalErr != nil {
				shared.RespondError(c, http.StatusInternalServerError, "Failed to create governance approval")
				return
			}
			ruleDoc["approvalRequired"] = true
			ruleDoc["approval"] = approvalDoc
		} else {
			operationID := "op-" + uuid.NewString()
			opDoc := bson.M{
				"_id":          operationID,
				"id":           operationID,
				"type":         "rule.publish",
				"resourceType": "rule",
				"resourceId":   ruleID,
				"status":       operations.StatusQueued,
				"createdAt":    now,
				"updatedAt":    now,
				"payload": bson.M{
					"internal":            internal,
					"external":            external,
					"environment":         environment,
					"prevGateways":        []string{},
					"nextGateways":        gateways,
					"prevStatus":          "draft",
					"prevLastPublishedAt": "",
				},
				"requestedBy": shared.AuthDisplayName(c),
			}
			if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
				log.Printf("[rule.create] ruleId=%s failed to queue operation: %v", ruleID, err)
				_ = shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, bson.M{
					"status":    "draft",
					"gateways":  []string{},
					"updatedAt": shared.NowISO(),
				})
				shared.RespondError(c, http.StatusInternalServerError, "Failed to queue rule publish")
				return
			}

			shared.PublishOperationWithDispatchError(ctx, operationID)
		}
	}

	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "rule.create",
		ResourceType: "rule",
		ResourceID:   ruleID,
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"serviceId":   serviceID,
			"environment": environment,
			"status":      status,
			"action":      shared.StringValue(policy["action"]),
		},
	})

	log.Printf("[rule.create] ruleId=%s serviceId=%s environment=%s name=%s", ruleID, serviceID, environment, name)
	c.JSON(http.StatusOK, ruleDoc)
}

func UpdateRule(c *gin.Context) {
	ruleID := c.Param("id")
	if ruleID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule ID required")
		return
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	existing, err := shared.FindOne(ctx, shared.Collection(shared.RulesCollection), bson.M{"id": ruleID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Rule not found")
		return
	}

	serviceName := ""
	serviceID := shared.StringValue(existing["serviceId"])
	if serviceID != "" {
		if service, serviceErr := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID}); serviceErr == nil {
			serviceName = shared.StringValue(service["name"])
		}
	}

	update := bson.M{"updatedAt": shared.NowISO()}
	if v, ok := payload["name"]; ok {
		update["name"] = shared.CanonicalRuleName(shared.StringValue(v), serviceName)
	}
	if v, ok := payload["paths"]; ok {
		update["paths"] = shared.ToStringSlice(v)
	}
	if v, ok := payload["methods"]; ok {
		update["methods"] = shared.ToStringSlice(v)
	}
	if v, ok := payload["hosts"]; ok {
		update["hosts"] = shared.ToStringSlice(v)
	}
	if v, ok := payload["protocol"]; ok {
		update["protocol"] = shared.StringValue(v)
	}
	if v, ok := payload["port"]; ok {
		update["port"] = shared.IntValue(v)
	}
	if v, ok := payload["policy"]; ok {
		update["policy"] = shared.MapPayload(v)
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update rule")
		return
	}

	updated, err := shared.FindOne(ctx, shared.Collection(shared.RulesCollection), bson.M{"id": ruleID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to fetch updated rule")
		return
	}
	log.Printf("[rule.update] ruleId=%s", ruleID)
	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "rule.update",
		ResourceType: "rule",
		ResourceID:   ruleID,
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"serviceId": shared.StringValue(updated["serviceId"]),
			"status":    shared.StringValue(updated["status"]),
		},
	})
	c.JSON(http.StatusOK, updated)
}

func DeleteRule(c *gin.Context) {
	ruleID := c.Param("id")
	if ruleID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	rule, err := shared.FindOne(ctx, shared.Collection(shared.RulesCollection), bson.M{"id": ruleID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Rule not found")
		return
	}

	status := shared.StringValue(rule["status"])
	if status == "publishing" || status == "unpublishing" || status == operations.StatusQueued || status == operations.StatusInProgress {
		shared.RespondError(c, http.StatusConflict, "Cannot delete rule while publish/unpublish is in progress")
		return
	}

	activeOperation, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{
		"type":       "rule.delete",
		"resourceId": ruleID,
		"status": bson.M{
			"$in": []string{operations.StatusQueued, operations.StatusInProgress},
		},
	})
	if err == nil {
		c.JSON(http.StatusAccepted, gin.H{"operation": activeOperation})
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) && err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to check delete queue")
		return
	}

	serviceID := shared.StringValue(rule["serviceId"])
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule service ID missing")
		return
	}
	environment := strings.TrimSpace(shared.StringValue(rule["environment"]))
	if environment == "" {
		environment = "prod"
	}
	serviceName := serviceID
	if service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID}); err == nil {
		if name := shared.StringValue(service["name"]); name != "" {
			serviceName = name
		}
	}
	ruleName := shared.StringValue(rule["name"])
	if ruleName == "" {
		ruleName = ruleID
	}
	ruleName = shared.CanonicalRuleName(ruleName, serviceName)
	if ruleName == "" {
		ruleName = ruleID
	}
	policyMap := shared.MapPayload(rule["policy"])
	action := shared.StringValue(policyMap["action"])
	if action == "" {
		action = "allow"
	}

	now := shared.NowISO()
	triggeredBy := shared.AuthDisplayName(c)
	if triggeredBy == "" {
		triggeredBy = "System"
	}

	ruleDeployID := "rdeploy-" + uuid.NewString()
	ruleDeployDoc := bson.M{
		"_id":         ruleDeployID,
		"id":          ruleDeployID,
		"ruleId":      ruleID,
		"serviceId":   serviceID,
		"status":      operations.StatusQueued,
		"environment": environment,
		"triggeredBy": triggeredBy,
		"startedAt":   now,
		"logs":        []interface{}{},
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.RuleDeploysCollection), ruleDeployDoc); err != nil {
		log.Printf("[rule.delete] ruleId=%s failed to create rule deploy: %v", ruleID, err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue rule delete")
		return
	}

	opID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          opID,
		"id":           opID,
		"type":         "rule.delete",
		"resourceType": "rule",
		"resourceId":   ruleID,
		"ruleDeployId": ruleDeployID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment": environment,
			"serviceId":   serviceID,
			"serviceName": serviceName,
			"ruleName":    ruleName,
			"action":      action,
		},
		"requestedBy": triggeredBy,
		"serviceName": serviceName,
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		log.Printf("[rule.delete] ruleId=%s failed to queue operation: %v", ruleID, err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue rule delete")
		return
	}
	_ = shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, bson.M{
		"status":    operations.StatusQueued,
		"updatedAt": now,
	})

	log.Printf("[rule.delete] ruleId=%s operationId=%s", ruleID, opID)

	if err := shared.PublishOperation(ctx, opID); err != nil {
		shared.RecordOperationDispatchError(ctx, opID, err)
		c.JSON(http.StatusAccepted, gin.H{
			"operation":  opDoc,
			"ruleDeploy": ruleDeployDoc,
			"warning":    "Worker queue unavailable. Operation remains queued.",
		})
		return
	}

	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "rule.delete.queued",
		ResourceType: "rule",
		ResourceID:   ruleID,
		Status:       "accepted",
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"serviceId":   serviceID,
			"operationId": opID,
		},
	})

	c.JSON(http.StatusAccepted, gin.H{"operation": opDoc, "ruleDeploy": ruleDeployDoc})
}

func PublishRule(c *gin.Context) {
	ruleID := c.Param("id")
	if ruleID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule ID required")
		return
	}

	var payload struct {
		Internal    bool   `json:"internal"`
		External    bool   `json:"external"`
		Environment string `json:"environment"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	rule, err := shared.FindOne(ctx, shared.Collection(shared.RulesCollection), bson.M{"id": ruleID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Rule not found")
		return
	}

	environment := strings.TrimSpace(payload.Environment)
	if environment == "" {
		environment = shared.StringValue(rule["environment"])
	}
	if environment == "" {
		environment = "prod"
	}
	environment = shared.NormalizeOperationEnvironment(environment)
	serviceID := shared.StringValue(rule["serviceId"])

	activeWorker, workerErr := shared.HasActiveWorkerForEnvironment(ctx, environment)
	if workerErr != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to validate worker availability")
		return
	}
	if !activeWorker {
		c.JSON(http.StatusConflict, gin.H{
			"message":     shared.WorkerUnavailableMessage(environment),
			"code":        shared.WorkerAvailabilityErrorCode,
			"environment": environment,
		})
		return
	}

	log.Printf("[rule.deploy] ruleId=%s serviceId=%s environment=%s internal=%v external=%v requestedBy=%s",
		ruleID, serviceID, environment, payload.Internal, payload.External, shared.AuthDisplayName(c))

	activeOperation, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{
		"type":       bson.M{"$in": []string{"rule.deploy", "rule.publish"}},
		"resourceId": ruleID,
		"status": bson.M{
			"$in": []string{operations.StatusQueued, operations.StatusInProgress},
		},
	})
	if err == nil {
		log.Printf("[rule.deploy] ruleId=%s already has active operation=%s", ruleID, shared.StringValue(activeOperation["id"]))
		c.JSON(http.StatusAccepted, gin.H{"operation": activeOperation})
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) && err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to check deploy queue")
		return
	}

	prevGateways := shared.ToStringSlice(rule["gateways"])
	nextGateways := operations.BuildGateways(prevGateways, payload.Internal, payload.External, environment)
	triggeredBy := shared.AuthDisplayName(c)
	if triggeredBy == "" {
		triggeredBy = "System"
	}

	settings, err := shared.LoadGovernanceSettings(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load governance settings")
		return
	}
	requiresApproval, minApprovers := shared.RulePublishApprovalRequired(settings, payload.External)
	if requiresApproval {
		ruleName := strings.TrimSpace(shared.StringValue(rule["name"]))
		if ruleName == "" {
			ruleName = ruleID
		}
		metadata := map[string]interface{}{
			"environment": environment,
			"serviceId":   serviceID,
			"hosts":       shared.ToStringSlice(rule["hosts"]),
			"gateways":    nextGateways,
			"action": map[string]interface{}{
				"kind":        "rule.publish",
				"ruleId":      ruleID,
				"serviceId":   serviceID,
				"environment": environment,
				"internal":    payload.Internal,
				"external":    payload.External,
				"requestedBy": triggeredBy,
			},
		}
		approvalDoc, existing, approvalErr := shared.CreateOrGetPendingGovernanceApproval(ctx, shared.GovernanceApprovalCreateParams{
			Type:         shared.GovernanceApprovalTypeRulePublish,
			ResourceID:   ruleID,
			ResourceName: ruleName,
			Environment:  environment,
			RequestedBy: bson.M{
				"id":    strings.TrimSpace(c.GetString("authUserId")),
				"name":  triggeredBy,
				"email": strings.TrimSpace(c.GetString("authEmail")),
			},
			Metadata:          metadata,
			RequiredApprovers: minApprovers,
		})
		if approvalErr != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to create governance approval")
			return
		}
		response := gin.H{
			"queued":            false,
			"approvalRequired":  true,
			"approval":          approvalDoc,
			"code":              "GOVERNANCE_APPROVAL_REQUIRED",
			"requiredApprovers": minApprovers,
			"message":           "Rule publish requires approval before queueing.",
		}
		if existing {
			response["alreadyPending"] = true
		}
		c.JSON(http.StatusAccepted, response)
		return
	}

	now := shared.NowISO()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, bson.M{
		"gateways":    nextGateways,
		"status":      operations.StatusQueued,
		"environment": environment,
		"updatedAt":   now,
	}); err != nil {
		log.Printf("[rule.deploy] ruleId=%s failed to update rule: %v", ruleID, err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update rule")
		return
	}

	ruleDeployID := "rdeploy-" + uuid.NewString()
	ruleDeployDoc := bson.M{
		"_id":         ruleDeployID,
		"id":          ruleDeployID,
		"ruleId":      ruleID,
		"serviceId":   serviceID,
		"status":      operations.StatusQueued,
		"environment": environment,
		"triggeredBy": triggeredBy,
		"startedAt":   now,
		"logs":        []interface{}{},
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.RuleDeploysCollection), ruleDeployDoc); err != nil {
		log.Printf("[rule.deploy] ruleId=%s failed to create rule deploy: %v", ruleID, err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create rule deploy")
		return
	}

	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         "rule.deploy",
		"resourceType": "rule",
		"resourceId":   ruleID,
		"ruleDeployId": ruleDeployID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"internal":            payload.Internal,
			"external":            payload.External,
			"environment":         environment,
			"prevGateways":        prevGateways,
			"nextGateways":        nextGateways,
			"prevStatus":          shared.StringValue(rule["status"]),
			"prevLastPublishedAt": shared.StringValue(rule["lastPublishedAt"]),
		},
		"requestedBy": triggeredBy,
		"serviceName": shared.StringValue(rule["serviceName"]),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		log.Printf("[rule.deploy] ruleId=%s failed to queue operation: %v", ruleID, err)
		revert := bson.M{
			"status":    shared.StringValue(rule["status"]),
			"gateways":  prevGateways,
			"updatedAt": shared.NowISO(),
		}
		if prevLast := shared.StringValue(rule["lastPublishedAt"]); prevLast != "" {
			revert["lastPublishedAt"] = prevLast
		} else {
			revert["lastPublishedAt"] = nil
		}
		_ = shared.UpdateByID(ctx, shared.Collection(shared.RulesCollection), ruleID, revert)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue rule deploy")
		return
	}

	log.Printf("[rule.deploy] ruleId=%s operationId=%s ruleDeployId=%s gateways=%v", ruleID, operationID, ruleDeployID, nextGateways)

	if err := shared.PublishOperation(ctx, operationID); err != nil {
		log.Printf("[rule.deploy] ruleId=%s operationId=%s dispatch error: %v", ruleID, operationID, err)
		shared.RecordOperationDispatchError(ctx, operationID, err)
		c.JSON(http.StatusAccepted, gin.H{
			"operation":  opDoc,
			"ruleDeploy": ruleDeployDoc,
			"warning":    "Worker queue unavailable. Operation remains queued.",
		})
		return
	}

	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "rule.publish.queued",
		ResourceType: "rule",
		ResourceID:   ruleID,
		Status:       "accepted",
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"serviceId":   serviceID,
			"environment": environment,
			"operationId": operationID,
			"gateways":    nextGateways,
		},
	})

	c.JSON(http.StatusAccepted, gin.H{"operation": opDoc, "ruleDeploy": ruleDeployDoc})
}
