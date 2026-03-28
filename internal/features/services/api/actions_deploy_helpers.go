package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	deploys "releaseaapi/internal/features/deploys/api"
	operations "releaseaapi/internal/features/operations/api"
	registryproviders "releaseaapi/internal/platform/providers/registry"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	secretsproviders "releaseaapi/internal/platform/providers/secrets"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type createDeployRequest struct {
	ServiceID   string
	Environment string
	Version     string
	Trigger     string
}

type promoteCanaryRequest struct {
	ServiceID   string
	Environment string
}

type deployResolution struct {
	Branch  string
	Version string
	Image   string
}

type deployPolicyEvaluationResult struct {
	Environment     string                                   `json:"environment"`
	Trigger         string                                   `json:"trigger"`
	SourceType      string                                   `json:"sourceType"`
	RegistryHost    string                                   `json:"registryHost,omitempty"`
	StrategyType    string                                   `json:"strategyType"`
	Replicas        int                                      `json:"replicas"`
	ExplicitVersion bool                                     `json:"explicitVersion"`
	Target          shared.GovernanceDeployPolicyTarget      `json:"target"`
	Violations      []shared.GovernanceDeployPolicyViolation `json:"violations"`
}

var loadGovernanceSettings = shared.LoadGovernanceSettings
var recordGovernancePolicyBlockAudit = func(
	ctx context.Context,
	serviceID string,
	serviceName string,
	performedBy bson.M,
	details bson.M,
) {
	auditID := "gaudit-" + uuid.NewString()
	doc := bson.M{
		"_id":          auditID,
		"id":           auditID,
		"action":       "governance.deploy_policy.blocked",
		"resourceType": "deploy",
		"resourceId":   serviceID,
		"resourceName": serviceName,
		"performedBy":  performedBy,
		"performedAt":  shared.NowISO(),
		"details":      details,
	}
	_ = shared.InsertOne(ctx, shared.Collection(shared.GovernanceAuditCollection), doc)
}

func recordPlatformDeployAudit(
	ctx context.Context,
	c *gin.Context,
	action string,
	resourceID string,
	status string,
	metadata map[string]interface{},
) {
	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       action,
		ResourceType: "deploy",
		ResourceID:   resourceID,
		Status:       status,
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata:     metadata,
	})
}

func resolvePerformedByForGovernanceAudit(c *gin.Context) bson.M {
	return bson.M{
		"id":    strings.TrimSpace(c.GetString("authUserId")),
		"name":  strings.TrimSpace(shared.AuthDisplayName(c)),
		"email": strings.TrimSpace(c.GetString("authEmail")),
	}
}

func parseCreateDeployRequest(c *gin.Context) (createDeployRequest, bool) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return createDeployRequest{}, false
	}

	var payload struct {
		Environment string `json:"environment"`
		Version     string `json:"version"`
		Trigger     string `json:"trigger"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return createDeployRequest{}, false
	}

	trigger := strings.ToLower(strings.TrimSpace(payload.Trigger))
	if trigger == "" {
		trigger = "manual"
	}

	return createDeployRequest{
		ServiceID:   serviceID,
		Environment: shared.NormalizeOperationEnvironment(payload.Environment),
		Version:     strings.TrimSpace(payload.Version),
		Trigger:     trigger,
	}, true
}

func loadDeployServiceOrRespond(c *gin.Context, ctx context.Context, serviceID string) (bson.M, bool) {
	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.LogWarn("service.deploy.service_not_found", shared.LogFields{
			"serviceId": serviceID,
			"error":     err.Error(),
		})
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return nil, false
	}
	return service, true
}

func resolveServiceDeployContextOrRespond(
	c *gin.Context,
	serviceID string,
	environment string,
	service bson.M,
) (string, string, bool, bool) {
	serviceType := strings.ToLower(shared.StringValue(service["type"]))
	if strings.EqualFold(shared.StringValue(service["status"]), "creating") {
		c.JSON(http.StatusConflict, gin.H{
			"message": "Service creation is still in progress. Wait until it finishes before deploying.",
			"code":    "SERVICE_CREATION_IN_PROGRESS",
		})
		return "", "", false, false
	}
	if isObservedService(service) {
		c.JSON(http.StatusConflict, gin.H{
			"message": "Observed services cannot be deployed by Releasea. Switch the service to managed mode first.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return "", "", false, false
	}

	sourceType := normalizeServiceSourceType(shared.StringValue(service["sourceType"]))
	if sourceType == "" {
		if strings.TrimSpace(shared.StringValue(service["repoUrl"])) != "" {
			sourceType = "git"
		} else if strings.TrimSpace(shared.StringValue(service["dockerImage"])) != "" {
			sourceType = "registry"
		}
	}

	isStaticSite := serviceType == "static-site"
	shared.LogInfo("service.deploy.context_resolved", shared.LogFields{
		"serviceId":        serviceID,
		"environment":      environment,
		"serviceType":      serviceType,
		"sourceType":       shared.StringValue(service["sourceType"]),
		"deployTemplateId": shared.StringValue(service["deployTemplateId"]),
	})
	return serviceType, sourceType, isStaticSite, true
}

func resolveDeployResolution(
	ctx context.Context,
	serviceID string,
	environment string,
	requestedVersion string,
	sourceType string,
	service bson.M,
) (deployResolution, error) {
	branch := strings.TrimSpace(shared.StringValue(service["branch"]))
	if branch == "" {
		branch = "main"
	}

	resolution := deployResolution{
		Branch:  branch,
		Version: strings.TrimSpace(requestedVersion),
	}

	if sourceType == "git" && isDeployVersionAlias(resolution.Version) {
		resolvedCommit, resolveErr := resolveLatestServiceCommitSHA(ctx, service, branch)
		if resolveErr != nil {
			shared.LogWarn("service.deploy.latest_commit_resolution_failed", shared.LogFields{
				"serviceId":   serviceID,
				"environment": environment,
				"branch":      branch,
				"error":       resolveErr.Error(),
			})
			resolution.Version = ""
		} else {
			resolution.Version = resolvedCommit
		}
	}

	if isRegistrySourceType(sourceType) {
		version, image, err := resolveRegistryDeployTarget(
			ctx,
			serviceID,
			environment,
			resolution.Version,
			shared.StringValue(service["dockerImage"]),
		)
		if err != nil {
			return deployResolution{}, err
		}
		resolution.Version = version
		resolution.Image = image
	}
	return resolution, nil
}

func respondIfActiveDeployBlocked(c *gin.Context, ctx context.Context, serviceID, environment string) (bool, error) {
	activeDeployFilter := bson.M{
		"serviceId":   serviceID,
		"environment": environment,
		"status": bson.M{
			"$in": operations.DeployQueueBlockingStatuses(),
		},
	}

	existingDeploy, err := shared.FindOne(ctx, shared.Collection(shared.DeploysCollection), activeDeployFilter)
	if err == nil {
		shared.LogInfo("service.deploy.blocked_by_active_deploy", shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
			"deployId":    shared.StringValue(existingDeploy["id"]),
			"status":      shared.StringValue(existingDeploy["status"]),
		})

		response := gin.H{"deploy": existingDeploy}
		if deployID := shared.StringValue(existingDeploy["id"]); deployID != "" {
			if existingOperation, opErr := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{
				"type":       operations.OperationTypeServiceDeploy,
				"resourceId": serviceID,
				"deployId":   deployID,
				"status": bson.M{
					"$in": []string{operations.StatusQueued, operations.StatusInProgress},
				},
			}); opErr == nil {
				response["operation"] = existingOperation
			}
		}
		response["queued"] = false
		response["blockedByActiveDeploy"] = true
		c.JSON(http.StatusAccepted, response)
		return true, nil
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return false, err
	}
	return false, nil
}

func resolveDeployResourcesOrRespond(
	c *gin.Context,
	ctx context.Context,
	serviceID string,
	service bson.M,
	environment string,
	isStaticSite bool,
) ([]map[string]interface{}, string, bool) {
	if isStaticSite {
		shared.LogInfo("service.deploy.static_site_skip_resources", shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
		})
		return nil, "", true
	}

	template, err := deploys.ResolveDeployTemplate(ctx, service)
	if err != nil {
		shared.LogError("service.deploy.resolve_template_failed", err, shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
		})
		shared.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to resolve deploy template: %v", err))
		return nil, "", false
	}
	if len(template) == 0 {
		shared.LogWarn("service.deploy.template_missing", shared.LogFields{
			"serviceId":        serviceID,
			"environment":      environment,
			"deployTemplateId": shared.StringValue(service["deployTemplateId"]),
			"sourceType":       shared.StringValue(service["sourceType"]),
		})
		shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf(
			"Deploy template missing for service %s (deployTemplateId=%s sourceType=%s)",
			serviceID,
			shared.StringValue(service["deployTemplateId"]),
			shared.StringValue(service["sourceType"]),
		))
		return nil, "", false
	}

	templateResources, err := deploys.ExtractTemplateResources(template)
	if err != nil {
		shared.LogWarn("service.deploy.template_invalid_resources", shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
			"templateId":  shared.StringValue(template["id"]),
			"error":       err.Error(),
		})
		shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Deploy template invalid: %v", err))
		return nil, "", false
	}
	if len(templateResources) == 0 {
		templateResources = repairEmptyTemplateResources(ctx, serviceID, environment, template)
	}
	if len(templateResources) == 0 {
		shared.LogWarn("service.deploy.template_no_resources", shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
			"templateId":  shared.StringValue(template["id"]),
		})
		shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf(
			"Deploy template %s has no resources",
			shared.StringValue(template["id"]),
		))
		return nil, "", false
	}

	resources, err := deploys.BuildDeployResources(ctx, service, environment)
	if err != nil {
		shared.LogError("service.deploy.build_resources_failed", err, shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
		})
		shared.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to prepare deploy resources: %v", err))
		return nil, "", false
	}
	if len(resources) == 0 {
		shared.LogWarn("service.deploy.build_resources_empty", shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
			"templateId":  shared.StringValue(template["id"]),
		})
		shared.RespondError(c, http.StatusInternalServerError, "Deploy template resources empty")
		return nil, "", false
	}
	shared.LogInfo("service.deploy.resources_built", shared.LogFields{
		"serviceId":   serviceID,
		"environment": environment,
		"resources":   len(resources),
		"templateId":  shared.StringValue(template["id"]),
	})

	namespace := shared.ResolveAppNamespace(environment)
	if err := shared.ValidateAppNamespace(namespace); err != nil {
		shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Deploy blocked: %v", err))
		return nil, "", false
	}
	resourcesWithNamespace := append([]map[string]interface{}{deploys.BuildNamespaceResource(namespace)}, resources...)
	resourcesYAML, err := deploys.RenderResourcesYAML(resourcesWithNamespace)
	if err != nil {
		shared.LogError("service.deploy.render_resources_failed", err, shared.LogFields{
			"serviceId":   serviceID,
			"environment": environment,
		})
		shared.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to render deploy resources: %v", err))
		return nil, "", false
	}

	return resources, resourcesYAML, true
}

func repairEmptyTemplateResources(ctx context.Context, serviceID, environment string, template bson.M) []map[string]interface{} {
	seeded := deploys.DefaultDeployTemplateResources(
		shared.StringValue(template["id"]),
		shared.StringValue(template["type"]),
	)
	if len(seeded) == 0 {
		return nil
	}

	templateID := shared.StringValue(template["_id"])
	if templateID == "" {
		templateID = shared.StringValue(template["id"])
	}
	if templateID != "" {
		if err := shared.UpdateByID(ctx, shared.Collection(shared.DeployTemplatesCollection), templateID, bson.M{
			"resources": seeded,
			"updatedAt": shared.NowISO(),
		}); err != nil {
			shared.LogWarn("service.deploy.template_repair_failed", shared.LogFields{
				"serviceId":   serviceID,
				"environment": environment,
				"templateId":  shared.StringValue(template["id"]),
				"error":       err.Error(),
			})
		} else {
			shared.LogInfo("service.deploy.template_repaired", shared.LogFields{
				"serviceId":   serviceID,
				"environment": environment,
				"templateId":  shared.StringValue(template["id"]),
			})
		}
	}

	templateResources, _ := deploys.ExtractTemplateResources(bson.M{"resources": seeded})
	return templateResources
}

func resolveDeployTriggeredBy(c *gin.Context, trigger string) string {
	triggeredBy := shared.AuthDisplayName(c)
	if triggeredBy == "" {
		triggeredBy = "System"
	}
	if trigger == "auto" {
		triggeredBy = "Auto Deploy"
	}
	return triggeredBy
}

func resolveDeployServiceName(serviceID string, service bson.M) string {
	serviceName := strings.TrimSpace(shared.StringValue(service["name"]))
	if serviceName == "" {
		serviceName = serviceID
	}
	return serviceName
}

func resolveServiceDeployReplicaTarget(service bson.M) int {
	replicas := shared.IntValue(service["replicas"])
	if replicas <= 0 {
		replicas = shared.IntValue(service["minReplicas"])
	}
	if replicas <= 0 {
		replicas = 1
	}
	return replicas
}

func maybeRespondDeployPolicyBlocked(
	c *gin.Context,
	ctx context.Context,
	serviceID string,
	serviceName string,
	service bson.M,
	environment string,
	trigger string,
	sourceType string,
	registryHost string,
	strategyType string,
	replicas int,
	explicitVersion bool,
) bool {
	evaluation, err := evaluateDeployPolicyWithResolvedInputs(
		ctx,
		service,
		environment,
		trigger,
		sourceType,
		registryHost,
		strategyType,
		replicas,
		explicitVersion,
	)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to resolve deploy policy context")
		return true
	}
	if len(evaluation.Violations) == 0 {
		return false
	}

	recordGovernancePolicyBlockAudit(ctx, serviceID, serviceName, resolvePerformedByForGovernanceAudit(c), bson.M{
		"environment":     evaluation.Environment,
		"trigger":         evaluation.Trigger,
		"sourceType":      evaluation.SourceType,
		"registryHost":    evaluation.RegistryHost,
		"strategyType":    evaluation.StrategyType,
		"replicas":        evaluation.Replicas,
		"explicitVersion": evaluation.ExplicitVersion,
		"target":          evaluation.Target,
		"violations":      evaluation.Violations,
	})

	c.JSON(http.StatusConflict, gin.H{
		"queued":     false,
		"code":       "GOVERNANCE_DEPLOY_POLICY_VIOLATION",
		"message":    evaluation.Violations[0].Message,
		"violations": evaluation.Violations,
	})
	return true
}

func evaluateDeployPolicyWithResolvedInputs(
	ctx context.Context,
	service bson.M,
	environment string,
	trigger string,
	sourceType string,
	registryHost string,
	strategyType string,
	replicas int,
	explicitVersion bool,
) (deployPolicyEvaluationResult, error) {
	result := deployPolicyEvaluationResult{
		Environment:     environment,
		Trigger:         trigger,
		SourceType:      sourceType,
		RegistryHost:    registryHost,
		StrategyType:    strategyType,
		Replicas:        replicas,
		ExplicitVersion: explicitVersion,
		Violations:      []shared.GovernanceDeployPolicyViolation{},
	}

	settings, err := loadGovernanceSettings(ctx)
	if err != nil {
		return result, err
	}
	target, err := resolveDeployPolicyTarget(ctx, service, sourceType)
	if err != nil {
		return result, err
	}
	result.Target = target
	result.Violations = shared.EvaluateDeployPolicy(
		settings,
		environment,
		trigger,
		strategyType,
		sourceType,
		registryHost,
		replicas,
		explicitVersion,
		target,
	)
	return result, nil
}

func resolveDeployPolicyTarget(ctx context.Context, service bson.M, sourceType string) (shared.GovernanceDeployPolicyTarget, error) {
	target := shared.GovernanceDeployPolicyTarget{
		ProfileID: strings.TrimSpace(shared.StringValue(service["profileId"])),
	}

	project, err := resolveDeployPolicyProject(ctx, service)
	if err != nil {
		return target, err
	}

	if normalizeServiceSourceType(sourceType) == "git" {
		scmCredential, err := resolveDeployPolicyCredential(
			ctx,
			shared.ScmCredentialsCollection,
			strings.TrimSpace(shared.StringValue(service["scmCredentialId"])),
			strings.TrimSpace(shared.StringValue(project["scmCredentialId"])),
		)
		if err != nil {
			return target, err
		}
		if len(scmCredential) > 0 {
			target.SCMProvider = scmproviders.Normalize(shared.StringValue(scmCredential["provider"]))
		}
	}

	if normalizeServiceSourceType(sourceType) == "registry" {
		registryCredential, err := resolveDeployPolicyCredential(
			ctx,
			shared.RegistryCredentialsCollection,
			strings.TrimSpace(shared.StringValue(service["registryCredentialId"])),
			strings.TrimSpace(shared.StringValue(project["registryCredentialId"])),
		)
		if err != nil {
			return target, err
		}
		if len(registryCredential) > 0 {
			target.RegistryProvider = registryproviders.Normalize(shared.StringValue(registryCredential["provider"]))
		}
	}

	if serviceUsesSecretRefs(service) {
		secretProvider, err := deploys.ResolveSecretProvider(ctx, service)
		if err != nil {
			return target, err
		}
		if len(secretProvider) > 0 {
			target.SecretProvider = secretsproviders.Normalize(shared.StringValue(secretProvider["type"]))
		}
	}

	return target, nil
}

func resolveDeployPolicyProject(ctx context.Context, service bson.M) (bson.M, error) {
	projectID := strings.TrimSpace(shared.StringValue(service["projectId"]))
	if projectID == "" {
		return bson.M{}, nil
	}
	project, err := shared.FindOne(ctx, shared.Collection(shared.ProjectsCollection), bson.M{"id": projectID})
	if errors.Is(err, mongo.ErrNoDocuments) {
		return bson.M{}, nil
	}
	return project, err
}

func resolveDeployPolicyCredential(ctx context.Context, collectionName, serviceCredentialID, projectCredentialID string) (bson.M, error) {
	if credential, found, err := resolveDeployPolicyCredentialByIDOrLegacyObjectID(ctx, collectionName, serviceCredentialID); err != nil {
		return nil, err
	} else if found {
		return credential, nil
	}
	if credential, found, err := resolveDeployPolicyCredentialByIDOrLegacyObjectID(ctx, collectionName, projectCredentialID); err != nil {
		return nil, err
	} else if found {
		return credential, nil
	}
	credential, err := shared.FindLatestPlatformCredential(ctx, collectionName)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return bson.M{}, nil
	}
	return credential, err
}

func resolveDeployPolicyCredentialByIDOrLegacyObjectID(ctx context.Context, collectionName, credentialID string) (bson.M, bool, error) {
	credentialID = strings.TrimSpace(credentialID)
	if credentialID == "" {
		return nil, false, nil
	}

	orFilters := []bson.M{
		{"id": credentialID},
		{"_id": credentialID},
	}
	if objectID, err := primitive.ObjectIDFromHex(credentialID); err == nil {
		orFilters = append(orFilters, bson.M{"_id": objectID})
	}

	document, err := shared.FindOne(ctx, shared.Collection(collectionName), bson.M{"$or": orFilters})
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return document, true, nil
}

func serviceUsesSecretRefs(service bson.M) bool {
	environment := shared.MapPayload(service["environment"])
	for _, raw := range environment {
		if secretsproviders.IsSecretRef(shared.StringValue(raw)) {
			return true
		}
	}
	return false
}

func maybeRespondDeployApprovalRequired(
	c *gin.Context,
	ctx context.Context,
	request createDeployRequest,
	resolution deployResolution,
	serviceName string,
	triggeredBy string,
	strategyType string,
	workerTags []string,
	resources []map[string]interface{},
	resourcesYAML string,
) bool {
	settings, err := loadGovernanceSettings(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load governance settings")
		return true
	}
	requiresApproval, minApprovers := shared.DeployApprovalRequired(settings, request.Environment)
	if !requiresApproval {
		return false
	}

	metadata := buildDeployApprovalMetadata(request, resolution, serviceName, triggeredBy, strategyType, workerTags, resources, resourcesYAML)
	requestedBy := buildDeployApprovalRequestedBy(c, triggeredBy)

	approvalDoc, existing, approvalErr := shared.CreateOrGetPendingGovernanceApproval(ctx, shared.GovernanceApprovalCreateParams{
		Type:              shared.GovernanceApprovalTypeDeploy,
		ResourceID:        request.ServiceID,
		ResourceName:      serviceName,
		Environment:       request.Environment,
		RequestedBy:       requestedBy,
		Metadata:          metadata,
		RequiredApprovers: minApprovers,
	})
	if approvalErr != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create governance approval")
		return true
	}

	response := gin.H{
		"queued":            false,
		"approvalRequired":  true,
		"approval":          approvalDoc,
		"code":              "GOVERNANCE_APPROVAL_REQUIRED",
		"requiredApprovers": minApprovers,
		"message":           "Deployment requires approval before queueing.",
	}
	if existing {
		response["alreadyPending"] = true
	}
	c.JSON(http.StatusAccepted, response)
	return true
}

func buildDeployApprovalMetadata(
	request createDeployRequest,
	resolution deployResolution,
	serviceName string,
	triggeredBy string,
	strategyType string,
	workerTags []string,
	resources []map[string]interface{},
	resourcesYAML string,
) map[string]interface{} {
	normalizedWorkerTags := shared.NormalizeWorkerTags(workerTags)
	action := map[string]interface{}{
		"kind":          operations.OperationTypeServiceDeploy,
		"serviceId":     request.ServiceID,
		"serviceName":   serviceName,
		"environment":   request.Environment,
		"version":       resolution.Version,
		"branch":        resolution.Branch,
		"commitSha":     resolution.Version,
		"strategyType":  strategyType,
		"trigger":       request.Trigger,
		"requestedBy":   triggeredBy,
		"resources":     resources,
		"resourcesYaml": resourcesYAML,
	}
	if len(normalizedWorkerTags) > 0 {
		action["workerTags"] = normalizedWorkerTags
	}

	metadata := map[string]interface{}{
		"version":     resolution.Version,
		"branch":      resolution.Branch,
		"commit":      resolution.Version,
		"environment": request.Environment,
		"serviceId":   request.ServiceID,
		"serviceName": serviceName,
		"trigger":     request.Trigger,
		"action":      action,
	}
	if len(normalizedWorkerTags) > 0 {
		metadata["workerTags"] = normalizedWorkerTags
	}
	if resolution.Image != "" {
		metadata["image"] = resolution.Image
		action["image"] = resolution.Image
	}
	return metadata
}

func buildDeployApprovalRequestedBy(c *gin.Context, triggeredBy string) bson.M {
	requestedBy := bson.M{
		"id":    strings.TrimSpace(c.GetString("authUserId")),
		"name":  triggeredBy,
		"email": strings.TrimSpace(c.GetString("authEmail")),
	}
	if shared.StringValue(requestedBy["id"]) == "" {
		requestedBy["id"] = strings.TrimSpace(shared.StringValue(requestedBy["name"]))
	}
	return requestedBy
}

func persistDeployRecord(
	ctx context.Context,
	service bson.M,
	request createDeployRequest,
	resolution deployResolution,
	triggeredBy string,
	now string,
) (bson.M, error) {
	deployID := "deploy-" + uuid.NewString()
	deployDoc := bson.M{
		"_id":            deployID,
		"id":             deployID,
		"serviceId":      request.ServiceID,
		"status":         operations.DeployStatusRequested,
		"environment":    request.Environment,
		"commit":         resolution.Version,
		"branch":         resolution.Branch,
		"triggeredBy":    triggeredBy,
		"trigger":        request.Trigger,
		"startedAt":      now,
		"logs":           []interface{}{},
		"strategyStatus": buildDeployStrategyStatus(service, operations.DeployStatusRequested, "Deployment requested", now),
	}
	if resolution.Image != "" {
		deployDoc["image"] = resolution.Image
	}

	if err := shared.InsertOne(ctx, shared.Collection(shared.DeploysCollection), deployDoc); err != nil {
		return nil, err
	}
	return deployDoc, nil
}

func persistDeployOperationRecord(
	ctx context.Context,
	request createDeployRequest,
	resolution deployResolution,
	strategyType string,
	workerTags []string,
	serviceName string,
	triggeredBy string,
	deployID string,
	resources []map[string]interface{},
	resourcesYAML string,
	now string,
) (bson.M, string, error) {
	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         operations.OperationTypeServiceDeploy,
		"resourceType": "service",
		"resourceId":   request.ServiceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment":  request.Environment,
			"version":      resolution.Version,
			"commitSha":    resolution.Version,
			"strategyType": strategyType,
			"trigger":      request.Trigger,
		},
		"deployId":    deployID,
		"requestedBy": triggeredBy,
		"serviceName": serviceName,
	}

	opPayload := shared.MapPayload(opDoc["payload"])
	if resolution.Image != "" {
		opPayload["image"] = resolution.Image
	}
	if normalizedWorkerTags := shared.NormalizeWorkerTags(workerTags); len(normalizedWorkerTags) > 0 {
		opPayload["workerTags"] = normalizedWorkerTags
	}
	opPayload["resources"] = resources
	if resourcesYAML != "" {
		opPayload["resourcesYaml"] = resourcesYAML
	}
	opDoc["payload"] = opPayload

	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return nil, "", err
	}
	return opDoc, operationID, nil
}

func parsePromoteCanaryRequest(c *gin.Context) (promoteCanaryRequest, bool) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return promoteCanaryRequest{}, false
	}

	var payload struct {
		Environment string `json:"environment"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return promoteCanaryRequest{}, false
	}

	return promoteCanaryRequest{
		ServiceID:   serviceID,
		Environment: shared.NormalizeOperationEnvironment(strings.TrimSpace(payload.Environment)),
	}, true
}

func respondIfPromoteCanaryBlocked(c *gin.Context, ctx context.Context, serviceID, environment string) (bool, error) {
	activeFilter := bson.M{
		"type":       operations.OperationTypeServicePromoteCanary,
		"resourceId": serviceID,
		"status": bson.M{
			"$in": []string{operations.StatusQueued, operations.StatusInProgress},
		},
		"payload.environment": environment,
	}

	activeOp, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), activeFilter)
	if err == nil {
		c.JSON(http.StatusAccepted, gin.H{"operation": activeOp, "message": "Promote already in progress"})
		return true, nil
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return false, err
	}
	return false, nil
}

func resolveRequestedByOrSystem(c *gin.Context) string {
	requestedBy := shared.AuthDisplayName(c)
	if requestedBy == "" {
		return "System"
	}
	return requestedBy
}

func persistPromoteCanaryOperation(
	ctx context.Context,
	serviceID string,
	environment string,
	workerTags []string,
	serviceName string,
	requestedBy string,
	now string,
) (bson.M, string, error) {
	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         operations.OperationTypeServicePromoteCanary,
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment": environment,
		},
		"requestedBy": requestedBy,
		"serviceName": serviceName,
	}
	if normalizedWorkerTags := shared.NormalizeWorkerTags(workerTags); len(normalizedWorkerTags) > 0 {
		shared.MapPayload(opDoc["payload"])["workerTags"] = normalizedWorkerTags
	}

	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return nil, "", err
	}
	return opDoc, operationID, nil
}
