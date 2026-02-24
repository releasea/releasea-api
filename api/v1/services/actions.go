package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"releaseaapi/api/v1/deploys"
	gh "releaseaapi/api/v1/integrations/github"
	"releaseaapi/api/v1/models"
	"releaseaapi/api/v1/operations"
	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func CreateDeploy(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	var payload struct {
		Environment string `json:"environment"`
		Version     string `json:"version"`
		Trigger     string `json:"trigger"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	payload.Environment = normalizeOperationEnvironment(payload.Environment)
	trigger := strings.ToLower(strings.TrimSpace(payload.Trigger))
	if trigger == "" {
		trigger = "manual"
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		log.Printf("[deploy] service=%s not found: %v", serviceID, err)
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	serviceType := strings.ToLower(shared.StringValue(service["type"]))
	if strings.EqualFold(shared.StringValue(service["status"]), "creating") {
		c.JSON(http.StatusConflict, gin.H{
			"message": "Service creation is still in progress. Wait until it finishes before deploying.",
			"code":    "SERVICE_CREATION_IN_PROGRESS",
		})
		return
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
	log.Printf("[deploy] service=%s env=%s type=%s sourceType=%s deployTemplateId=%s", serviceID, payload.Environment, serviceType, shared.StringValue(service["sourceType"]), shared.StringValue(service["deployTemplateId"]))

	deployBranch := strings.TrimSpace(shared.StringValue(service["branch"]))
	if deployBranch == "" {
		deployBranch = "main"
	}
	deployVersion := strings.TrimSpace(payload.Version)
	deployImage := ""
	if sourceType == "git" && isDeployVersionAlias(deployVersion) {
		resolvedCommit, resolveErr := resolveLatestServiceCommitSHA(ctx, service, deployBranch)
		if resolveErr != nil {
			log.Printf("[deploy] service=%s env=%s failed to resolve latest github commit for branch=%s: %v", serviceID, payload.Environment, deployBranch, resolveErr)
			deployVersion = ""
		} else {
			deployVersion = resolvedCommit
		}
	}
	if isRegistrySourceType(sourceType) {
		resolvedVersion, resolvedImage, resolveErr := resolveRegistryDeployTarget(
			ctx,
			serviceID,
			payload.Environment,
			deployVersion,
			shared.StringValue(service["dockerImage"]),
		)
		if resolveErr != nil {
			log.Printf("[deploy] service=%s env=%s failed to resolve registry image target: %v", serviceID, payload.Environment, resolveErr)
			shared.RespondError(c, http.StatusBadRequest, "Unable to resolve deploy image for this Docker source.")
			return
		}
		deployVersion = resolvedVersion
		deployImage = resolvedImage
	}

	activeDeployFilter := bson.M{
		"serviceId":   serviceID,
		"environment": payload.Environment,
		"status": bson.M{
			"$in": operations.DeployQueueBlockingStatuses(),
		},
	}

	existingDeploy, err := shared.FindOne(ctx, shared.Collection(shared.DeploysCollection), activeDeployFilter)
	if err == nil {
		log.Printf("[deploy] service=%s env=%s blocked by deploy=%s status=%s", serviceID, payload.Environment, shared.StringValue(existingDeploy["id"]), shared.StringValue(existingDeploy["status"]))
		response := gin.H{"deploy": existingDeploy}
		if deployID := shared.StringValue(existingDeploy["id"]); deployID != "" {
			if existingOperation, opErr := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{
				"type":       "service.deploy",
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
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) && err != nil {
		log.Printf("[deploy] service=%s env=%s failed to check deploy queue: %v", serviceID, payload.Environment, err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to check deploy queue")
		return
	}
	if !ensureWorkerAvailabilityOrRespond(c, ctx, payload.Environment) {
		return
	}

	var resources []map[string]interface{}
	var resourcesYaml string
	if !isStaticSite {
		template, err := deploys.ResolveDeployTemplate(ctx, service)
		if err != nil {
			log.Printf("[deploy] service=%s env=%s resolve template error: %v", serviceID, payload.Environment, err)
			shared.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to resolve deploy template: %v", err))
			return
		}
		if len(template) == 0 {
			log.Printf("[deploy] service=%s env=%s missing deploy template (deployTemplateId=%s sourceType=%s)", serviceID, payload.Environment, shared.StringValue(service["deployTemplateId"]), shared.StringValue(service["sourceType"]))
			shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf(
				"Deploy template missing for service %s (deployTemplateId=%s sourceType=%s)",
				serviceID,
				shared.StringValue(service["deployTemplateId"]),
				shared.StringValue(service["sourceType"]),
			))
			return
		}
		templateResources, err := deploys.ExtractTemplateResources(template)
		if err != nil {
			log.Printf("[deploy] service=%s env=%s template=%s invalid resources: %v", serviceID, payload.Environment, shared.StringValue(template["id"]), err)
			shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Deploy template invalid: %v", err))
			return
		}
		if len(templateResources) == 0 {
			seeded := deploys.DefaultDeployTemplateResources(shared.StringValue(template["id"]), shared.StringValue(template["type"]))
			if len(seeded) > 0 {
				templateID := shared.StringValue(template["_id"])
				if templateID == "" {
					templateID = shared.StringValue(template["id"])
				}
				if templateID != "" {
					if err := shared.UpdateByID(ctx, shared.Collection(shared.DeployTemplatesCollection), templateID, bson.M{
						"resources": seeded,
						"updatedAt": shared.NowISO(),
					}); err != nil {
						log.Printf("[deploy] service=%s env=%s template=%s failed to repair resources: %v", serviceID, payload.Environment, shared.StringValue(template["id"]), err)
					} else {
						log.Printf("[deploy] service=%s env=%s template=%s repaired with default resources", serviceID, payload.Environment, shared.StringValue(template["id"]))
					}
				}
				templateResources, _ = deploys.ExtractTemplateResources(bson.M{"resources": seeded})
			}
		}
		if len(templateResources) == 0 {
			log.Printf("[deploy] service=%s env=%s template=%s has no resources", serviceID, payload.Environment, shared.StringValue(template["id"]))
			shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf(
				"Deploy template %s has no resources",
				shared.StringValue(template["id"]),
			))
			return
		}

		resources, err = deploys.BuildDeployResources(ctx, service, payload.Environment)
		if err != nil {
			log.Printf("[deploy] service=%s env=%s build resources error: %v", serviceID, payload.Environment, err)
			shared.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to prepare deploy resources: %v", err))
			return
		}
		if len(resources) == 0 {
			log.Printf("[deploy] service=%s env=%s build resources empty (template=%s)", serviceID, payload.Environment, shared.StringValue(template["id"]))
			shared.RespondError(c, http.StatusInternalServerError, "Deploy template resources empty")
			return
		}
		log.Printf("[deploy] service=%s env=%s resources=%d (template=%s)", serviceID, payload.Environment, len(resources), shared.StringValue(template["id"]))

		namespace := shared.ResolveAppNamespace(payload.Environment)
		if err := shared.ValidateAppNamespace(namespace); err != nil {
			shared.RespondError(c, http.StatusBadRequest, fmt.Sprintf("Deploy blocked: %v", err))
			return
		}
		resourcesWithNamespace := append([]map[string]interface{}{deploys.BuildNamespaceResource(namespace)}, resources...)
		resourcesYaml, err = deploys.RenderResourcesYAML(resourcesWithNamespace)
		if err != nil {
			log.Printf("[deploy] service=%s env=%s render resources error: %v", serviceID, payload.Environment, err)
			shared.RespondError(c, http.StatusInternalServerError, fmt.Sprintf("Failed to render deploy resources: %v", err))
			return
		}
	} else {
		log.Printf("[deploy] service=%s env=%s static site deploy, skipping template resources", serviceID, payload.Environment)
	}

	triggeredBy := shared.AuthDisplayName(c)
	if triggeredBy == "" {
		triggeredBy = "System"
	}
	if trigger == "auto" {
		triggeredBy = "Auto Deploy"
	}
	serviceName := strings.TrimSpace(shared.StringValue(service["name"]))
	if serviceName == "" {
		serviceName = serviceID
	}
	strategyType := resolveServiceDeployStrategyType(service)

	settings, err := shared.LoadGovernanceSettings(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load governance settings")
		return
	}
	requiresApproval, minApprovers := shared.DeployApprovalRequired(settings, payload.Environment)
	if requiresApproval {
		metadata := map[string]interface{}{
			"version":     deployVersion,
			"branch":      deployBranch,
			"commit":      deployVersion,
			"environment": payload.Environment,
			"serviceId":   serviceID,
			"serviceName": serviceName,
			"trigger":     trigger,
			"action": map[string]interface{}{
				"kind":          "service.deploy",
				"serviceId":     serviceID,
				"serviceName":   serviceName,
				"environment":   payload.Environment,
				"version":       deployVersion,
				"branch":        deployBranch,
				"commitSha":     deployVersion,
				"strategyType":  strategyType,
				"trigger":       trigger,
				"requestedBy":   triggeredBy,
				"resources":     resources,
				"resourcesYaml": resourcesYaml,
			},
		}
		if deployImage != "" {
			metadata["image"] = deployImage
			shared.MapPayload(metadata["action"])["image"] = deployImage
		}
		requestedBy := bson.M{
			"id":    strings.TrimSpace(c.GetString("authUserId")),
			"name":  triggeredBy,
			"email": strings.TrimSpace(c.GetString("authEmail")),
		}
		if shared.StringValue(requestedBy["id"]) == "" {
			requestedBy["id"] = strings.TrimSpace(shared.StringValue(requestedBy["name"]))
		}
		approvalDoc, existing, approvalErr := shared.CreateOrGetPendingGovernanceApproval(ctx, shared.GovernanceApprovalCreateParams{
			Type:              shared.GovernanceApprovalTypeDeploy,
			ResourceID:        serviceID,
			ResourceName:      serviceName,
			Environment:       payload.Environment,
			RequestedBy:       requestedBy,
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
			"message":           "Deployment requires approval before queueing.",
		}
		if existing {
			response["alreadyPending"] = true
		}
		c.JSON(http.StatusAccepted, response)
		return
	}

	deployID := "deploy-" + uuid.NewString()
	now := shared.NowISO()

	deployDoc := bson.M{
		"_id":            deployID,
		"id":             deployID,
		"serviceId":      serviceID,
		"status":         operations.DeployStatusRequested,
		"environment":    payload.Environment,
		"commit":         deployVersion,
		"branch":         deployBranch,
		"triggeredBy":    triggeredBy,
		"trigger":        trigger,
		"startedAt":      now,
		"logs":           []interface{}{},
		"strategyStatus": buildDeployStrategyStatus(service, operations.DeployStatusRequested, "Deployment requested", now),
	}
	if deployImage != "" {
		deployDoc["image"] = deployImage
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.DeploysCollection), deployDoc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create deploy")
		return
	}

	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         "service.deploy",
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment":  payload.Environment,
			"version":      deployVersion,
			"commitSha":    deployVersion,
			"strategyType": strategyType,
			"trigger":      trigger,
		},
		"deployId":    deployID,
		"requestedBy": triggeredBy,
		"serviceName": serviceName,
	}
	opPayload := shared.MapPayload(opDoc["payload"])
	if deployImage != "" {
		opPayload["image"] = deployImage
	}
	opPayload["resources"] = resources
	if resourcesYaml != "" {
		opPayload["resourcesYaml"] = resourcesYaml
	}
	opDoc["payload"] = opPayload
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue deploy")
		return
	}

	if !publishOperationOrRespondQueued(c, ctx, operationID, opDoc, gin.H{"deploy": deployDoc}) {
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"operation": opDoc, "deploy": deployDoc, "queued": true})
}

func PromoteCanary(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}
	var payload struct {
		Environment string `json:"environment"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil && !errors.Is(err, io.EOF) {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	environment := strings.TrimSpace(payload.Environment)
	environment = normalizeOperationEnvironment(environment)

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}
	strategyType := resolveServiceDeployStrategyType(service)
	if strategyType != "canary" {
		shared.RespondError(c, http.StatusBadRequest, "Service is not using canary strategy")
		return
	}

	activeFilter := bson.M{
		"type":       "service.promote-canary",
		"resourceId": serviceID,
		"status": bson.M{
			"$in": []string{operations.StatusQueued, operations.StatusInProgress},
		},
		"payload.environment": environment,
	}
	activeOp, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), activeFilter)
	if err == nil {
		c.JSON(http.StatusAccepted, gin.H{"operation": activeOp, "message": "Promote already in progress"})
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) && err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to check promote queue")
		return
	}
	if !ensureWorkerAvailabilityOrRespond(c, ctx, environment) {
		return
	}

	now := shared.NowISO()
	stableCanaryPercent := 0
	if err := operations.RepublishRulesForServiceStrategyWithOptions(ctx, serviceID, operations.RuleStrategyRepublishOptions{
		Environment:           environment,
		CanaryPercentOverride: &stableCanaryPercent,
	}); err != nil {
		log.Printf("[service] failed to republish rules after canary promote (service=%s): %v", serviceID, err)
	}

	operationID := "op-" + uuid.NewString()
	requestedBy := shared.AuthDisplayName(c)
	if requestedBy == "" {
		requestedBy = "System"
	}
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         "service.promote-canary",
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment": environment,
		},
		"requestedBy": requestedBy,
		"serviceName": shared.StringValue(service["name"]),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue promote")
		return
	}
	if !publishOperationOrRespondQueued(c, ctx, operationID, opDoc, nil) {
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"operation": opDoc})
}

func publishOperationOrRespondQueued(c *gin.Context, ctx context.Context, operationID string, opDoc bson.M, extra gin.H) bool {
	err := shared.PublishOperation(ctx, operationID)
	if err == nil {
		return true
	}

	shared.RecordOperationDispatchError(ctx, operationID, err)
	response := gin.H{
		"operation": opDoc,
		"warning":   "Worker queue unavailable. Operation remains queued.",
	}
	for key, value := range extra {
		response[key] = value
	}
	c.JSON(http.StatusAccepted, response)
	return false
}

func buildDeployStrategyStatus(service bson.M, phase, summary, now string) bson.M {
	strategy := shared.MapPayload(service["deploymentStrategy"])
	strategyType := resolveServiceDeployStrategyType(service)

	details := bson.M{}
	switch strategyType {
	case "canary":
		canaryPercent := shared.IntValue(strategy["canaryPercent"])
		if canaryPercent <= 0 {
			canaryPercent = 10
		}
		if canaryPercent > 50 {
			canaryPercent = 50
		}
		details["exposurePercent"] = canaryPercent
		details["stablePercent"] = 100 - canaryPercent
	case "blue-green":
		primary := strings.ToLower(strings.TrimSpace(shared.StringValue(strategy["blueGreenPrimary"])))
		if primary != "green" {
			primary = "blue"
		}
		details["activeSlot"] = primary
		if primary == "blue" {
			details["inactiveSlot"] = "green"
		} else {
			details["inactiveSlot"] = "blue"
		}
	default:
		replicas := shared.IntValue(service["replicas"])
		if replicas <= 0 {
			replicas = shared.IntValue(service["minReplicas"])
		}
		if replicas <= 0 {
			replicas = 1
		}
		details["targetReplicas"] = replicas
	}

	return bson.M{
		"type":      strategyType,
		"phase":     phase,
		"summary":   summary,
		"details":   details,
		"updatedAt": now,
	}
}

func resolveServiceDeployStrategyType(service bson.M) string {
	if strings.EqualFold(shared.StringValue(service["deployTemplateId"]), "tpl-cronjob") {
		return "rolling"
	}
	strategy := shared.MapPayload(service["deploymentStrategy"])
	strategyType := strings.ToLower(strings.TrimSpace(shared.StringValue(strategy["type"])))
	switch strategyType {
	case "canary", "blue-green", "rolling":
		return strategyType
	default:
		return "rolling"
	}
}

func isDeployVersionAlias(version string) bool {
	switch strings.ToLower(strings.TrimSpace(version)) {
	case "", "latest", "head":
		return true
	default:
		return isLatestImageReference(version)
	}
}

func isLatestImageReference(version string) bool {
	normalized := strings.ToLower(strings.TrimSpace(version))
	return normalized != "" && strings.HasSuffix(normalized, ":latest")
}

func normalizeServiceSourceType(sourceType string) string {
	switch strings.ToLower(strings.TrimSpace(sourceType)) {
	case "registry", "docker":
		return "registry"
	case "git":
		return "git"
	default:
		return ""
	}
}

func isRegistrySourceType(sourceType string) bool {
	return normalizeServiceSourceType(sourceType) == "registry"
}

func resolveRegistryDeployTarget(
	ctx context.Context,
	serviceID string,
	environment string,
	requestedVersion string,
	serviceImage string,
) (string, string, error) {
	version := strings.TrimSpace(requestedVersion)
	image := strings.TrimSpace(serviceImage)

	if isDeployVersionAlias(version) {
		resolvedVersion, resolvedImage, err := resolveLatestServiceBuildVersion(ctx, serviceID, environment, image)
		if err == nil {
			return resolvedVersion, resolvedImage, nil
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			log.Printf("[deploy] service=%s env=%s latest build lookup failed, using configured image fallback: %v", serviceID, environment, err)
		}
		if image == "" {
			return "", "", errors.New("docker image is not configured for this service")
		}
		fallbackVersion := extractImageTagOrDigest(image)
		if fallbackVersion == "" {
			fallbackVersion = "latest"
		}
		return fallbackVersion, image, nil
	}

	if isImageReference(version) {
		imageVersion := extractImageTagOrDigest(version)
		if imageVersion == "" {
			imageVersion = version
		}
		return imageVersion, version, nil
	}

	resolvedVersion, resolvedImage, err := resolveServiceBuildVersion(ctx, serviceID, environment, version, image)
	if err == nil {
		return resolvedVersion, resolvedImage, nil
	}
	if err != nil && !errors.Is(err, mongo.ErrNoDocuments) {
		return "", "", err
	}

	if image == "" {
		return "", "", errors.New("docker image is not configured for this service")
	}
	taggedImage := withImageTag(image, version)
	if taggedImage == "" {
		return "", "", errors.New("failed to compute docker image tag from configured image")
	}
	return version, taggedImage, nil
}

func resolveLatestServiceBuildVersion(
	ctx context.Context,
	serviceID string,
	environment string,
	fallbackImage string,
) (string, string, error) {
	filter := bson.M{
		"serviceId": serviceID,
		"tag": bson.M{
			"$nin": []string{"", "latest"},
		},
	}
	normalizedEnvironment := strings.TrimSpace(environment)
	if normalizedEnvironment != "" {
		filter["environment"] = normalizedEnvironment
	}

	build, err := findLatestBuild(ctx, filter)
	if err != nil && errors.Is(err, mongo.ErrNoDocuments) && normalizedEnvironment != "" {
		delete(filter, "environment")
		build, err = findLatestBuild(ctx, filter)
	}
	if err != nil {
		return "", "", err
	}

	version := strings.TrimSpace(shared.StringValue(build["tag"]))
	if version == "" {
		version = strings.TrimSpace(shared.StringValue(build["shortSha"]))
	}
	if version == "" {
		version = strings.TrimSpace(shared.StringValue(build["commit"]))
	}
	if version == "" {
		return "", "", errors.New("latest build version is empty")
	}

	image := strings.TrimSpace(shared.StringValue(build["image"]))
	if image == "" {
		image = withImageTag(fallbackImage, version)
	}
	if image == "" {
		return "", "", errors.New("latest build image is empty")
	}

	return version, image, nil
}

func resolveServiceBuildVersion(
	ctx context.Context,
	serviceID string,
	environment string,
	version string,
	fallbackImage string,
) (string, string, error) {
	filter := bson.M{
		"serviceId": serviceID,
		"$or": []bson.M{
			{"tag": version},
			{"shortSha": version},
			{"commit": version},
		},
	}
	normalizedEnvironment := strings.TrimSpace(environment)
	if normalizedEnvironment != "" {
		filter["environment"] = normalizedEnvironment
	}

	build, err := findLatestBuild(ctx, filter)
	if err != nil && errors.Is(err, mongo.ErrNoDocuments) && normalizedEnvironment != "" {
		delete(filter, "environment")
		build, err = findLatestBuild(ctx, filter)
	}
	if err != nil {
		return "", "", err
	}

	resolvedVersion := strings.TrimSpace(shared.StringValue(build["tag"]))
	if resolvedVersion == "" {
		resolvedVersion = version
	}
	image := strings.TrimSpace(shared.StringValue(build["image"]))
	if image == "" {
		image = withImageTag(fallbackImage, resolvedVersion)
	}
	if image == "" {
		return "", "", errors.New("build image is empty")
	}
	return resolvedVersion, image, nil
}

func findLatestBuild(ctx context.Context, filter bson.M) (bson.M, error) {
	var build bson.M
	err := shared.Collection(shared.BuildsCollection).
		FindOne(ctx, filter, options.FindOne().SetSort(bson.D{{Key: "createdAt", Value: -1}})).
		Decode(&build)
	return build, err
}

func withImageTag(image, tag string) string {
	base := strings.TrimSpace(image)
	if base == "" || strings.TrimSpace(tag) == "" {
		return ""
	}

	if digestIdx := strings.Index(base, "@"); digestIdx >= 0 {
		base = base[:digestIdx]
	}
	lastColon := strings.LastIndex(base, ":")
	lastSlash := strings.LastIndex(base, "/")
	if lastColon > lastSlash {
		base = base[:lastColon]
	}
	if base == "" {
		return ""
	}
	return base + ":" + strings.TrimSpace(tag)
}

func extractImageTagOrDigest(image string) string {
	trimmed := strings.TrimSpace(image)
	if trimmed == "" {
		return ""
	}
	if at := strings.Index(trimmed, "@"); at >= 0 {
		return strings.TrimSpace(trimmed[at+1:])
	}
	lastColon := strings.LastIndex(trimmed, ":")
	lastSlash := strings.LastIndex(trimmed, "/")
	if lastColon > lastSlash && lastColon+1 < len(trimmed) {
		return strings.TrimSpace(trimmed[lastColon+1:])
	}
	return ""
}

func isImageReference(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if strings.Contains(trimmed, "@sha256:") {
		return true
	}
	lastColon := strings.LastIndex(trimmed, ":")
	lastSlash := strings.LastIndex(trimmed, "/")
	return lastSlash >= 0 && lastColon > lastSlash
}

func resolveLatestServiceCommitSHA(ctx context.Context, service bson.M, branch string) (string, error) {
	repoURL := strings.TrimSpace(shared.StringValue(service["repoUrl"]))
	repo, ok := gh.ParseRepo(repoURL)
	if !ok {
		return "", errors.New("repository URL is not a valid GitHub repository")
	}

	project, err := loadServiceProject(ctx, service)
	if err != nil {
		return "", err
	}

	credential, err := resolveServiceScmCredential(ctx, service, project)
	if err != nil {
		return "", err
	}
	if credential == nil {
		return "", errors.New("SCM credential not found")
	}
	token := strings.TrimSpace(shared.StringValue(credential["token"]))
	if token == "" {
		return "", errors.New("SCM credential missing token")
	}

	provider := strings.ToLower(strings.TrimSpace(shared.StringValue(credential["provider"])))
	if provider != "" && provider != "github" {
		return "", fmt.Errorf("unsupported SCM provider %q", provider)
	}

	apiURL := fmt.Sprintf(
		"https://api.github.com/repos/%s/%s/commits/%s",
		url.PathEscape(repo.Owner),
		url.PathEscape(repo.Name),
		url.PathEscape(branch),
	)
	body, status, err := gh.Request(ctx, token, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("GitHub request failed: %w", err)
	}
	if err := gh.ResponseError(status, body, "Failed to fetch latest commit"); err != nil {
		return "", err
	}

	var commitResponse models.GitHubCommitHeadResponse
	if err := json.Unmarshal(body, &commitResponse); err != nil {
		return "", fmt.Errorf("failed to parse latest commit response: %w", err)
	}

	sha := strings.TrimSpace(commitResponse.Sha)
	if sha == "" {
		return "", errors.New("latest commit SHA missing")
	}
	return sha, nil
}

func loadServiceProject(ctx context.Context, service bson.M) (bson.M, error) {
	projectID := strings.TrimSpace(shared.StringValue(service["projectId"]))
	if projectID == "" {
		return nil, nil
	}
	project, err := shared.FindOne(ctx, shared.Collection(shared.ProjectsCollection), bson.M{"id": projectID})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, nil
		}
		return nil, err
	}
	return project, nil
}
