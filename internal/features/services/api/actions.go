package services

import (
	"context"
	"errors"
	"net/http"
	"strings"

	operations "releaseaapi/internal/features/operations/api"
	scmproviders "releaseaapi/internal/platform/providers/scm"
	operationqueue "releaseaapi/internal/platform/queue"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func CreateDeploy(c *gin.Context) {
	request, ok := parseCreateDeployRequest(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, ok := loadDeployServiceOrRespond(c, ctx, request.ServiceID)
	if !ok {
		return
	}

	_, sourceType, isStaticSite, ok := resolveServiceDeployContextOrRespond(c, request.ServiceID, request.Environment, service)
	if !ok {
		return
	}

	resolution, err := resolveDeployResolution(ctx, request.ServiceID, request.Environment, request.Version, sourceType, service)
	if err != nil {
		shared.LogError("service.deploy.resolve_target_failed", err, shared.LogFields{
			"serviceId":   request.ServiceID,
			"environment": request.Environment,
		})
		shared.RespondError(c, http.StatusBadRequest, "Unable to resolve deploy image for this Docker source.")
		return
	}

	blocked, queueCheckErr := respondIfActiveDeployBlocked(c, ctx, request.ServiceID, request.Environment)
	if blocked {
		return
	}
	if queueCheckErr != nil {
		shared.LogError("service.deploy.queue_check_failed", queueCheckErr, shared.LogFields{
			"serviceId":   request.ServiceID,
			"environment": request.Environment,
		})
		shared.RespondError(c, http.StatusInternalServerError, "Failed to check deploy queue")
		return
	}
	if !ensureWorkerAvailabilityOrRespond(c, ctx, request.Environment) {
		return
	}

	resources, resourcesYAML, ok := resolveDeployResourcesOrRespond(c, ctx, request.ServiceID, service, request.Environment, isStaticSite)
	if !ok {
		return
	}

	triggeredBy := resolveDeployTriggeredBy(c, request.Trigger)
	serviceName := resolveDeployServiceName(request.ServiceID, service)
	strategyType := resolveServiceDeployStrategyType(service)

	if maybeRespondDeployApprovalRequired(c, ctx, request, resolution, serviceName, triggeredBy, strategyType, resources, resourcesYAML) {
		return
	}

	now := shared.NowISO()
	deployDoc, err := persistDeployRecord(ctx, service, request, resolution, triggeredBy, now)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create deploy")
		return
	}

	deployID := shared.StringValue(deployDoc["id"])
	opDoc, operationID, err := persistDeployOperationRecord(
		ctx,
		request,
		resolution,
		strategyType,
		serviceName,
		triggeredBy,
		deployID,
		resources,
		resourcesYAML,
		now,
	)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue deploy")
		return
	}

	if !publishOperationOrRespondQueued(c, ctx, operationID, opDoc, gin.H{"deploy": deployDoc}) {
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"operation": opDoc, "deploy": deployDoc, "queued": true})
}

func PromoteCanary(c *gin.Context) {
	request, ok := parsePromoteCanaryRequest(c)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, ok := loadDeployServiceOrRespond(c, ctx, request.ServiceID)
	if !ok {
		return
	}
	if isObservedService(service) {
		c.JSON(http.StatusConflict, gin.H{
			"message": "Observed services cannot be deployed by Releasea. Switch the service to managed mode first.",
			"code":    "SERVICE_OBSERVED_MODE",
		})
		return
	}
	strategyType := resolveServiceDeployStrategyType(service)
	if strategyType != "canary" {
		shared.RespondError(c, http.StatusBadRequest, "Service is not using canary strategy")
		return
	}

	blocked, queueCheckErr := respondIfPromoteCanaryBlocked(c, ctx, request.ServiceID, request.Environment)
	if blocked {
		return
	}
	if queueCheckErr != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to check promote queue")
		return
	}
	if !ensureWorkerAvailabilityOrRespond(c, ctx, request.Environment) {
		return
	}

	now := shared.NowISO()
	stableCanaryPercent := 0
	if err := operations.RepublishRulesForServiceStrategyWithOptions(ctx, request.ServiceID, operations.RuleStrategyRepublishOptions{
		Environment:           request.Environment,
		CanaryPercentOverride: &stableCanaryPercent,
	}); err != nil {
		shared.LogError("service.promote_canary.rules_republish_failed", err, shared.LogFields{
			"serviceId": request.ServiceID,
		})
	}

	requestedBy := resolveRequestedByOrSystem(c)
	opDoc, operationID, err := persistPromoteCanaryOperation(ctx, request.ServiceID, request.Environment, shared.StringValue(service["name"]), requestedBy, now)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue promote")
		return
	}
	if !publishOperationOrRespondQueued(c, ctx, operationID, opDoc, nil) {
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"operation": opDoc})
}

func publishOperationOrRespondQueued(c *gin.Context, ctx context.Context, operationID string, opDoc bson.M, extra gin.H) bool {
	err := operationqueue.PublishOperation(ctx, operationID)
	if err == nil {
		return true
	}

	operationqueue.RecordOperationDispatchError(ctx, operationID, err)
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

func normalizeServiceManagementMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "managed":
		return "managed"
	case "observed":
		return "observed"
	default:
		return ""
	}
}

func isObservedService(service bson.M) bool {
	return normalizeServiceManagementMode(shared.StringValue(service["managementMode"])) == "observed"
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
			shared.LogWarn("service.deploy.latest_build_lookup_failed", shared.LogFields{
				"serviceId":   serviceID,
				"environment": environment,
				"error":       err.Error(),
			})
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
		FindOne(ctx, filter, options.FindOne().SetSort(bson.D{bson.E{Key: "createdAt", Value: -1}})).
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
	runtime, err := scmproviders.ResolveRuntimeForCapability(provider, scmproviders.CapabilityCommitLookup)
	if err != nil {
		return "", err
	}

	return runtime.LatestCommitSHA(ctx, token, repoURL, branch)
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
