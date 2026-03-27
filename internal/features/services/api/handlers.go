package services

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	observability "releaseaapi/internal/features/observability/api"
	operations "releaseaapi/internal/features/operations/api"
	operationqueue "releaseaapi/internal/platform/queue"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

const (
	defaultPauseIdleTimeoutFallbackSeconds = 3600
	minPauseIdleTimeoutSeconds             = 60
	maxPauseIdleTimeoutSeconds             = 7 * 24 * 60 * 60
)

// Services

func GetServices(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	filter := bson.M{}
	if rawValue := strings.TrimSpace(c.Query("autoDeploy")); rawValue != "" {
		if enabled, ok := parseServicesQueryBool(rawValue); ok {
			if enabled {
				filter["$or"] = []bson.M{
					{"autoDeploy": true},
					{"autoDeploy": bson.M{"$exists": false}},
				}
			} else {
				filter["autoDeploy"] = false
			}
		}
	}
	if sourceType := normalizeServiceSourceType(c.Query("sourceType")); sourceType != "" {
		filter["sourceType"] = sourceType
	}
	if serviceType := strings.ToLower(strings.TrimSpace(c.Query("type"))); serviceType != "" {
		filter["type"] = serviceType
	}
	if projectID := strings.TrimSpace(c.Query("projectId")); projectID != "" {
		filter["projectId"] = projectID
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		filter["status"] = status
	} else if excludedStatuses := splitServicesQueryCSV(c.Query("excludeStatus")); len(excludedStatuses) > 0 {
		filter["status"] = bson.M{"$nin": excludedStatuses}
	}

	services, err := shared.FindAll(ctx, shared.Collection(shared.ServicesCollection), filter)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load services")
		return
	}
	c.JSON(http.StatusOK, services)
}

func parseServicesQueryBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y":
		return true, true
	case "0", "false", "no", "n":
		return false, true
	default:
		return false, false
	}
}

func splitServicesQueryCSV(raw string) []string {
	parts := strings.Split(strings.TrimSpace(raw), ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}

func GetService(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}
	c.JSON(http.StatusOK, service)
}

func CreateService(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if _, ok := payload["port"]; !ok {
		if servicePort, ok := payload["servicePort"]; ok {
			payload["port"] = servicePort
		}
	}
	delete(payload, "servicePort")
	if rawSourceType := shared.StringValue(payload["sourceType"]); rawSourceType != "" {
		if normalized := normalizeServiceSourceType(rawSourceType); normalized != "" {
			payload["sourceType"] = normalized
		} else {
			delete(payload, "sourceType")
		}
	}
	if rawManagementMode := strings.TrimSpace(shared.StringValue(payload["managementMode"])); rawManagementMode != "" {
		normalized := normalizeServiceManagementMode(rawManagementMode)
		if normalized == "" {
			shared.RespondError(c, http.StatusBadRequest, "Invalid management mode")
			return
		}
		payload["managementMode"] = normalized
	}

	id := "svc-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	payload["createdAt"] = shared.NowISO()
	if _, ok := payload["status"]; !ok {
		payload["status"] = "pending"
	}
	if _, ok := payload["environment"]; !ok {
		payload["environment"] = bson.M{}
	}
	if _, ok := payload["ruleIds"]; !ok {
		payload["ruleIds"] = []interface{}{}
	}
	if _, ok := payload["managementMode"]; !ok {
		payload["managementMode"] = "managed"
	}
	if _, ok := payload["sourceType"]; !ok {
		if shared.StringValue(payload["repoUrl"]) != "" {
			payload["sourceType"] = "git"
		} else if shared.StringValue(payload["dockerImage"]) != "" {
			payload["sourceType"] = "registry"
		}
	}
	if normalizeServiceSourceType(shared.StringValue(payload["sourceType"])) == "registry" &&
		strings.TrimSpace(shared.StringValue(payload["dockerImage"])) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Docker image required for Docker source")
		return
	}
	if _, ok := payload["deployTemplateId"]; !ok {
		payload["deployTemplateId"] = resolveDeployTemplateID(payload)
	}
	if _, ok := payload["autoDeploy"]; !ok {
		payload["autoDeploy"] = true
	}
	if normalizeServiceManagementMode(shared.StringValue(payload["managementMode"])) == "observed" {
		payload["autoDeploy"] = false
	}
	if _, ok := payload["pauseOnIdle"]; !ok {
		payload["pauseOnIdle"] = false
	}
	serviceType := strings.ToLower(shared.StringValue(payload["type"]))
	if serviceType != "microservice" {
		payload["pauseOnIdle"] = false
	} else if _, ok := payload["pauseIdleTimeoutSeconds"]; !ok {
		payload["pauseIdleTimeoutSeconds"] = pauseIdleDefaultTimeoutSeconds()
	}
	normalizeServiceRuntimeFields(payload)

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.ServicesCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create service")
		return
	}
	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "service.create",
		ResourceType: "service",
		ResourceID:   id,
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"name":       shared.StringValue(payload["name"]),
			"type":       shared.StringValue(payload["type"]),
			"sourceType": shared.StringValue(payload["sourceType"]),
			"projectId":  shared.StringValue(payload["projectId"]),
		},
	})
	c.JSON(http.StatusOK, payload)
}

func UpdateService(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	existing, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if strings.ToLower(shared.StringValue(existing["type"])) != "microservice" {
		payload["pauseOnIdle"] = false
	}

	scheduleFieldsPresent := hasSchedulePayload(payload)
	scheduleChanged := scheduleFieldsPresent && schedulePayloadChanged(existing, payload)
	if _, ok := payload["port"]; !ok {
		if servicePort, ok := payload["servicePort"]; ok {
			payload["port"] = servicePort
		}
	}
	delete(payload, "servicePort")
	if rawSourceType := shared.StringValue(payload["sourceType"]); rawSourceType != "" {
		if normalized := normalizeServiceSourceType(rawSourceType); normalized != "" {
			payload["sourceType"] = normalized
		} else {
			delete(payload, "sourceType")
		}
	}
	if rawManagementMode := strings.TrimSpace(shared.StringValue(payload["managementMode"])); rawManagementMode != "" {
		normalized := normalizeServiceManagementMode(rawManagementMode)
		if normalized == "" {
			shared.RespondError(c, http.StatusBadRequest, "Invalid management mode")
			return
		}
		payload["managementMode"] = normalized
	}
	if _, ok := payload["deployTemplateId"]; !ok {
		if _, hasSource := payload["sourceType"]; hasSource {
			payload["deployTemplateId"] = resolveDeployTemplateID(payload)
		}
	}
	if scheduleFieldsPresent {
		payload["deployTemplateId"] = "tpl-cronjob"
	}
	normalizeServiceRuntimeFields(payload)
	previousStrategy := normalizedServiceStrategyFromRaw(existing["deploymentStrategy"])
	nextStrategy := normalizedServiceStrategy{}
	strategyPayloadProvided := false
	if rawStrategy, ok := payload["deploymentStrategy"]; ok {
		nextStrategy = normalizedServiceStrategyFromRaw(rawStrategy)
		strategyPayloadProvided = nextStrategy.Type != ""
	}
	strategyChanged := strategyPayloadProvided && previousStrategy != nextStrategy

	// If repo URL changes or sourceType switches away from git, drop managed repo flag.
	if nextRepoURL, ok := payload["repoUrl"]; ok {
		prevRepoURL := shared.StringValue(existing["repoUrl"])
		if shared.StringValue(nextRepoURL) != "" && shared.StringValue(nextRepoURL) != prevRepoURL {
			payload["repoManaged"] = false
		}
	}
	if nextSource := normalizeServiceSourceType(shared.StringValue(payload["sourceType"])); nextSource == "registry" {
		payload["repoManaged"] = false
	}
	scaleEnv := strings.TrimSpace(shared.StringValue(payload["scaleEnvironment"]))
	delete(payload, "scaleEnvironment")
	if normalizeServiceManagementMode(shared.StringValue(payload["managementMode"])) == "observed" {
		payload["autoDeploy"] = false
	}
	payload["updatedAt"] = shared.NowISO()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update service")
		return
	}
	updated, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"_id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service")
		return
	}
	if newPort, ok := payload["port"]; ok {
		oldPort := shared.IntValue(existing["port"])
		nextPort := shared.IntValue(newPort)
		if nextPort > 0 && nextPort != oldPort {
			if err := operations.RepublishRulesForServicePortChange(ctx, serviceID, nextPort); err != nil {
				log.Printf("[service] failed to republish rules after port change (service=%s): %v", serviceID, err)
			}
		}
	}
	if strategyChanged {
		if err := operations.RepublishRulesForServiceStrategy(ctx, serviceID); err != nil {
			log.Printf("[service] failed to republish rules after strategy change (service=%s): %v", serviceID, err)
		}
	}

	// Queue immediate scale when replicas or instance type change.
	if replicasOrInstanceChanged(existing, payload) {
		env := scaleEnv
		if env == "" {
			env = "prod"
		}
		queueImmediateScale(ctx, c, updated, env)
	}

	if scheduleChanged && isCronJobService(updated) {
		triggeredBy := shared.AuthDisplayName(c)
		if triggeredBy == "" {
			triggeredBy = "System"
		}
		if err := queueCronJobScheduleDeploys(ctx, updated, triggeredBy); err != nil {
			log.Printf("[service] schedule update deploy queue failed (service=%s): %v", serviceID, err)
		}
	}
	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "service.update",
		ResourceType: "service",
		ResourceID:   serviceID,
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"name":             shared.StringValue(updated["name"]),
			"status":           shared.StringValue(updated["status"]),
			"deployTemplateId": shared.StringValue(updated["deployTemplateId"]),
		},
	})
	c.JSON(http.StatusOK, updated)
}

func resolveDeployTemplateID(payload bson.M) string {
	if shared.StringValue(payload["scheduleCron"]) != "" || shared.StringValue(payload["scheduleCommand"]) != "" {
		return "tpl-cronjob"
	}
	sourceType := normalizeServiceSourceType(shared.StringValue(payload["sourceType"]))
	if sourceType == "" {
		if shared.StringValue(payload["repoUrl"]) != "" {
			sourceType = "git"
		} else if shared.StringValue(payload["dockerImage"]) != "" {
			sourceType = "registry"
		}
	}
	if isRegistrySourceType(sourceType) {
		return "tpl-registry"
	}
	if sourceType == "git" {
		return "tpl-git"
	}
	return ""
}

func normalizeServiceRuntimeFields(payload bson.M) {
	resolveProfileResources(payload)
	normalizeReplicaBounds(payload)
	normalizePauseIdleTimeout(payload)
	normalizeDeploymentStrategy(payload)
}

func normalizePauseIdleTimeout(payload bson.M) {
	raw, ok := payload["pauseIdleTimeoutSeconds"]
	if !ok {
		return
	}
	timeoutSeconds := shared.IntValue(raw)
	if timeoutSeconds <= 0 {
		payload["pauseIdleTimeoutSeconds"] = pauseIdleDefaultTimeoutSeconds()
		return
	}
	if timeoutSeconds < minPauseIdleTimeoutSeconds {
		timeoutSeconds = minPauseIdleTimeoutSeconds
	}
	if timeoutSeconds > maxPauseIdleTimeoutSeconds {
		timeoutSeconds = maxPauseIdleTimeoutSeconds
	}
	payload["pauseIdleTimeoutSeconds"] = timeoutSeconds
}

func pauseIdleDefaultTimeoutSeconds() int {
	value := strings.TrimSpace(shared.EnvOrDefault("RELEASEA_PAUSE_IDLE_DEFAULT_SECONDS", strconv.Itoa(defaultPauseIdleTimeoutFallbackSeconds)))
	timeoutSeconds, err := strconv.Atoi(value)
	if err != nil || timeoutSeconds <= 0 {
		timeoutSeconds = defaultPauseIdleTimeoutFallbackSeconds
	}
	if timeoutSeconds < minPauseIdleTimeoutSeconds {
		timeoutSeconds = minPauseIdleTimeoutSeconds
	}
	if timeoutSeconds > maxPauseIdleTimeoutSeconds {
		timeoutSeconds = maxPauseIdleTimeoutSeconds
	}
	return timeoutSeconds
}

// resolveProfileResources looks up the runtime profile by profileId and sets cpu/memory on the payload.
func resolveProfileResources(payload bson.M) {
	profileID := shared.StringValue(payload["profileId"])
	if profileID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	profile, err := shared.FindOne(ctx, shared.Collection(shared.RuntimeProfilesCollection), bson.M{"_id": profileID})
	if err != nil || profile == nil {
		return
	}
	if cpu := parseResourceMillis(shared.StringValue(profile["cpu"])); cpu > 0 {
		payload["cpu"] = cpu
	}
	if mem := parseResourceMi(shared.StringValue(profile["memory"])); mem > 0 {
		payload["memory"] = mem
	}
}

// parseResourceMillis parses a K8s CPU string like "500m" or "2000m" into millicores int.
func parseResourceMillis(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	s = strings.TrimSuffix(s, "m")
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

// parseResourceMi parses a K8s memory string like "512Mi", "2048Mi" or "8Gi" into MiB int.
func parseResourceMi(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	if strings.HasSuffix(s, "Gi") {
		s = strings.TrimSuffix(s, "Gi")
		v, err := strconv.Atoi(s)
		if err != nil {
			return 0
		}
		return v * 1024
	}
	s = strings.TrimSuffix(s, "Mi")
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func replicasOrInstanceChanged(existing bson.M, payload bson.M) bool {
	if _, ok := payload["profileId"]; ok {
		prev := strings.TrimSpace(shared.StringValue(existing["profileId"]))
		next := strings.TrimSpace(shared.StringValue(payload["profileId"]))
		if next != "" && next != prev {
			return true
		}
	}
	if _, ok := payload["minReplicas"]; ok {
		if shared.IntValue(payload["minReplicas"]) != shared.IntValue(existing["minReplicas"]) {
			return true
		}
	}
	if _, ok := payload["maxReplicas"]; ok {
		if shared.IntValue(payload["maxReplicas"]) != shared.IntValue(existing["maxReplicas"]) {
			return true
		}
	}
	if _, ok := payload["cpu"]; ok {
		if shared.IntValue(payload["cpu"]) != shared.IntValue(existing["cpu"]) {
			return true
		}
	}
	if _, ok := payload["memory"]; ok {
		if shared.IntValue(payload["memory"]) != shared.IntValue(existing["memory"]) {
			return true
		}
	}
	return false
}

func queueImmediateScale(ctx context.Context, c *gin.Context, service bson.M, environment string) {
	serviceID := shared.StringValue(service["id"])
	replicas := shared.IntValue(service["replicas"])
	if replicas <= 0 {
		replicas = shared.IntValue(service["minReplicas"])
	}
	if replicas <= 0 {
		replicas = 1
	}
	now := shared.NowISO()
	requestedBy := shared.AuthDisplayName(c)
	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         "service.scale",
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment": environment,
			"replicas":    replicas,
			"action":      "scale",
			"cpu":         shared.IntValue(service["cpu"]),
			"memory":      shared.IntValue(service["memory"]),
		},
		"requestedBy": requestedBy,
		"serviceName": shared.StringValue(service["name"]),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		log.Printf("[service] failed to queue immediate scale (service=%s): %v", serviceID, err)
		return
	}
	if err := operationqueue.PublishOperation(ctx, operationID); err != nil {
		log.Printf("[service] failed to publish immediate scale (service=%s): %v", serviceID, err)
	}
}

func normalizeReplicaBounds(payload bson.M) {
	minReplicas := 0
	maxReplicas := 0
	if raw, ok := payload["minReplicas"]; ok {
		minReplicas = shared.IntValue(raw)
		if minReplicas < 1 {
			minReplicas = 1
		}
		payload["minReplicas"] = minReplicas
	}
	if raw, ok := payload["maxReplicas"]; ok {
		maxReplicas = shared.IntValue(raw)
		if maxReplicas < 1 {
			maxReplicas = 1
		}
		payload["maxReplicas"] = maxReplicas
	}
	if minReplicas > 0 && maxReplicas > 0 && maxReplicas < minReplicas {
		payload["maxReplicas"] = minReplicas
		maxReplicas = minReplicas
	}
	if minReplicas > 0 {
		if _, exists := payload["replicas"]; !exists {
			payload["replicas"] = minReplicas
		}
	}
}

func normalizeDeploymentStrategy(payload bson.M) {
	strategyType := strings.ToLower(strings.TrimSpace(shared.StringValue(payload["deployStrategyType"])))
	if strategyType == "" {
		if strategy, ok := normalizeStrategyMap(payload["deploymentStrategy"]); ok {
			payload["deploymentStrategy"] = strategy
			delete(payload, "deployStrategyType")
			delete(payload, "canaryPercent")
			delete(payload, "blueGreenPrimary")
			return
		}
	}
	if strategyType == "" {
		delete(payload, "deployStrategyType")
		delete(payload, "canaryPercent")
		delete(payload, "blueGreenPrimary")
		return
	}
	strategy := bson.M{"type": "rolling"}
	switch strategyType {
	case "rolling":
		strategy["type"] = "rolling"
	case "canary":
		canaryPercent := shared.IntValue(payload["canaryPercent"])
		if canaryPercent <= 0 {
			canaryPercent = 10
		}
		if canaryPercent > 50 {
			canaryPercent = 50
		}
		strategy["type"] = "canary"
		strategy["canaryPercent"] = canaryPercent
	case "blue-green":
		primary := strings.ToLower(strings.TrimSpace(shared.StringValue(payload["blueGreenPrimary"])))
		if primary != "green" {
			primary = "blue"
		}
		strategy["type"] = "blue-green"
		strategy["blueGreenPrimary"] = primary
	default:
		strategy["type"] = "rolling"
	}
	payload["deploymentStrategy"] = strategy
	delete(payload, "deployStrategyType")
	delete(payload, "canaryPercent")
	delete(payload, "blueGreenPrimary")
}

func normalizeStrategyMap(raw interface{}) (bson.M, bool) {
	switch value := raw.(type) {
	case bson.M:
		return sanitizeStrategy(value)
	case map[string]interface{}:
		return sanitizeStrategy(bson.M(value))
	default:
		return nil, false
	}
}

func sanitizeStrategy(value bson.M) (bson.M, bool) {
	strategyType := strings.ToLower(strings.TrimSpace(shared.StringValue(value["type"])))
	if strategyType == "" {
		return nil, false
	}
	clean := bson.M{}
	switch strategyType {
	case "rolling":
		clean["type"] = "rolling"
	case "canary":
		canaryPercent := shared.IntValue(value["canaryPercent"])
		if canaryPercent <= 0 {
			canaryPercent = 10
		}
		if canaryPercent > 50 {
			canaryPercent = 50
		}
		clean["type"] = "canary"
		clean["canaryPercent"] = canaryPercent
	case "blue-green":
		primary := strings.ToLower(strings.TrimSpace(shared.StringValue(value["blueGreenPrimary"])))
		if primary != "green" {
			primary = "blue"
		}
		clean["type"] = "blue-green"
		clean["blueGreenPrimary"] = primary
	default:
		return nil, false
	}
	return clean, true
}

type normalizedServiceStrategy struct {
	Type             string
	CanaryPercent    int
	BlueGreenPrimary string
}

func normalizedServiceStrategyFromRaw(raw interface{}) normalizedServiceStrategy {
	strategy, ok := normalizeStrategyMap(raw)
	if !ok {
		return normalizedServiceStrategy{}
	}
	out := normalizedServiceStrategy{
		Type: strings.ToLower(strings.TrimSpace(shared.StringValue(strategy["type"]))),
	}
	switch out.Type {
	case "canary":
		out.CanaryPercent = shared.IntValue(strategy["canaryPercent"])
	case "blue-green":
		primary := strings.ToLower(strings.TrimSpace(shared.StringValue(strategy["blueGreenPrimary"])))
		if primary != "green" {
			primary = "blue"
		}
		out.BlueGreenPrimary = primary
	}
	return out
}

func hasSchedulePayload(payload bson.M) bool {
	for _, key := range []string{
		"scheduleCron",
		"scheduleTimezone",
		"scheduleCommand",
		"scheduleRetries",
		"scheduleTimeout",
	} {
		if _, ok := payload[key]; ok {
			return true
		}
	}
	return false
}

func schedulePayloadChanged(existing bson.M, payload bson.M) bool {
	for _, key := range []string{
		"scheduleCron",
		"scheduleTimezone",
		"scheduleCommand",
		"scheduleRetries",
		"scheduleTimeout",
	} {
		if _, ok := payload[key]; !ok {
			continue
		}
		prev := normalizeScheduleValue(existing[key])
		next := normalizeScheduleValue(payload[key])
		if prev != next {
			return true
		}
	}
	return false
}

func normalizeScheduleValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case int:
		return strconv.Itoa(v)
	case int32:
		return strconv.Itoa(int(v))
	case int64:
		return strconv.FormatInt(v, 10)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return ""
	}
}

func isCronJobService(service bson.M) bool {
	return strings.EqualFold(shared.StringValue(service["deployTemplateId"]), "tpl-cronjob")
}

func queueCronJobScheduleDeploys(ctx context.Context, service bson.M, triggeredBy string) error {
	serviceID := shared.StringValue(service["id"])
	if serviceID == "" {
		serviceID = shared.StringValue(service["_id"])
	}
	if serviceID == "" {
		return nil
	}

	envs := collectSuccessfulDeployEnvironments(ctx, serviceID)
	if len(envs) == 0 {
		return nil
	}

	for _, env := range envs {
		if err := queueServiceDeployOperation(ctx, service, env, triggeredBy); err != nil {
			return err
		}
	}
	return nil
}

func collectSuccessfulDeployEnvironments(ctx context.Context, serviceID string) []string {
	deploys, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), bson.M{
		"serviceId": serviceID,
		"status": bson.M{
			"$in": operations.DeploySuccessfulStatuses(),
		},
	})
	if err != nil {
		return nil
	}
	envs := make(map[string]struct{})
	for _, deploy := range deploys {
		env := strings.TrimSpace(shared.StringValue(deploy["environment"]))
		if env == "" {
			env = "prod"
		}
		envs[env] = struct{}{}
	}
	ordered := make([]string, 0, len(envs))
	for env := range envs {
		ordered = append(ordered, env)
	}
	return ordered
}

func queueServiceDeployOperation(ctx context.Context, service bson.M, environment, triggeredBy string) error {
	serviceID := shared.StringValue(service["id"])
	if serviceID == "" {
		serviceID = shared.StringValue(service["_id"])
	}
	if serviceID == "" {
		return nil
	}
	environment = shared.NormalizeOperationEnvironment(environment)
	if err := ensureActiveWorkerForEnvironment(ctx, environment); err != nil {
		return err
	}
	activeDeploys, err := shared.Collection(shared.DeploysCollection).CountDocuments(ctx, bson.M{
		"serviceId":   serviceID,
		"environment": environment,
		"status": bson.M{
			"$in": operations.DeployQueueBlockingStatuses(),
		},
	})
	if err != nil {
		return err
	}
	if activeDeploys > 0 {
		return nil
	}

	deployID := "deploy-" + uuid.NewString()
	now := shared.NowISO()
	serviceName := shared.StringValue(service["name"])
	if serviceName == "" {
		serviceName = serviceID
	}
	branch := shared.StringValue(service["branch"])
	if branch == "" {
		branch = "main"
	}
	deployDoc := bson.M{
		"_id":            deployID,
		"id":             deployID,
		"serviceId":      serviceID,
		"status":         operations.DeployStatusRequested,
		"environment":    environment,
		"commit":         "",
		"branch":         branch,
		"triggeredBy":    triggeredBy,
		"startedAt":      now,
		"logs":           []interface{}{},
		"strategyStatus": buildDeployStrategyStatus(service, operations.DeployStatusRequested, "Deployment requested", now),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.DeploysCollection), deployDoc); err != nil {
		return err
	}

	operationID := "op-" + uuid.NewString()
	opDoc := bson.M{
		"_id":          operationID,
		"id":           operationID,
		"type":         operations.OperationTypeServiceDeploy,
		"resourceType": "service",
		"resourceId":   serviceID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"environment":  environment,
			"version":      "",
			"strategyType": resolveServiceDeployStrategyType(service),
		},
		"deployId":    deployID,
		"requestedBy": triggeredBy,
		"serviceName": serviceName,
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		return err
	}

	if err := operationqueue.PublishOperation(ctx, operationID); err != nil {
		operationqueue.RecordOperationDispatchError(ctx, operationID, err)
		return err
	}
	return nil
}

func DeleteService(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	activeDeploys, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), bson.M{
		"serviceId": serviceID,
		"status": bson.M{
			"$in": operations.DeployNonTerminalStatuses(),
		},
	})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to validate active deploys")
		return
	}
	for _, deploy := range activeDeploys {
		if deployBlocksServiceDeletion(deploy) {
			shared.RespondError(c, http.StatusConflict, "Service has deploys in progress")
			return
		}
	}

	rules, err := shared.FindAll(ctx, shared.Collection(shared.RulesCollection), bson.M{"serviceId": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service rules")
		return
	}
	for _, rule := range rules {
		status := shared.StringValue(rule["status"])
		if status == "publishing" || status == "unpublishing" || status == operations.StatusQueued || status == operations.StatusInProgress {
			shared.RespondError(c, http.StatusConflict, "Cannot delete service while rules are publishing")
			return
		}
	}

	now := shared.NowISO()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, bson.M{
		"status":    "deleting",
		"isActive":  false,
		"updatedAt": now,
	}); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to mark service for deletion")
		return
	}

	triggeredBy := shared.AuthDisplayName(c)
	if triggeredBy == "" {
		triggeredBy = "System"
	}

	for _, rule := range rules {
		if err := queueRuleDelete(ctx, rule, service, triggeredBy); err != nil {
			shared.RespondError(c, http.StatusBadGateway, err.Error())
			return
		}
	}

	environments := collectServiceEnvironments(ctx, serviceID, rules)
	for _, env := range environments {
		if err := queueServiceDelete(ctx, service, env, triggeredBy); err != nil {
			shared.RespondError(c, http.StatusBadGateway, err.Error())
			return
		}
	}

	actorID, actorName, actorRole := shared.AuditActorFromContext(c)
	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       "service.delete.queued",
		ResourceType: "service",
		ResourceID:   serviceID,
		Status:       "accepted",
		ActorID:      actorID,
		ActorName:    actorName,
		ActorRole:    actorRole,
		Metadata: map[string]interface{}{
			"name": shared.StringValue(service["name"]),
		},
	})

	c.JSON(http.StatusAccepted, gin.H{"status": "deleting"})
}

func deployBlocksServiceDeletion(deploy bson.M) bool {
	status := operations.NormalizeDeployStatus(shared.StringValue(deploy["status"]))
	if status == "" {
		return false
	}
	if status != operations.DeployStatusRollback {
		return true
	}
	return strings.TrimSpace(shared.StringValue(deploy["finishedAt"])) == ""
}

func GetServiceMetrics(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}
	serviceType := strings.ToLower(shared.StringValue(service["type"]))

	environment := strings.TrimSpace(c.Query("environment"))
	if environment == "" {
		shared.RespondError(c, http.StatusBadRequest, "environment query parameter required")
		return
	}
	serviceName := shared.ToKubeName(shared.StringValue(service["name"]))
	if serviceName == "" {
		serviceName = shared.ToKubeName(serviceID)
	}
	if serviceName == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service name invalid")
		return
	}

	start, end, step := observability.ParseMetricsRange(c.Query("from"), c.Query("to"))
	timestamps := observability.BuildTimestamps(start, end, step)
	timestampValues := make([]string, len(timestamps))
	for i, ts := range timestamps {
		timestampValues[i] = ts.Format(time.RFC3339)
	}

	namespace := shared.ResolveAppNamespace(environment)
	metricsNamespace := namespace
	promURL := observability.PrometheusURL()

	// Log request for diagnostics
	log.Printf("[metrics] service=%s env=%s namespace=%s serviceName=%s from=%s to=%s",
		serviceID, environment, namespace, serviceName, start.Format(time.RFC3339), end.Format(time.RFC3339))

	var cpuSamples, memSamples, latencySamples, reqSamples []observability.PromSample
	var status2xxSamples, status4xxSamples, status5xxSamples []observability.PromSample
	var cpuErr, memErr, latencyErr, reqErr error

	if serviceType == "static-site" {
		staticNamespace := shared.EnvOrDefault("RELEASEA_SYSTEM_NAMESPACE", "releasea-system")
		staticWorkload := shared.EnvOrDefault("RELEASEA_STATIC_NGINX_WORKLOAD", "releasea-static-nginx")
		staticScope := strings.ToLower(shared.EnvOrDefault("RELEASEA_STATIC_METRICS_SCOPE", "aggregate"))
		metricsNamespace = staticNamespace
		hostFilter := ""
		if staticScope == "host" || staticScope == "per-host" {
			hostFilter = buildStaticSiteHostFilter(ctx, service, serviceID, environment)
			if hostFilter == "" {
				hostFilter = buildStaticHostFallbackFilter(serviceName)
			}
		}
		staticServiceFqdn := fmt.Sprintf("%s.%s.svc.cluster.local", staticWorkload, staticNamespace)
		labelVariants := buildIstioLabelVariants(staticWorkload, staticNamespace, staticServiceFqdn)

		requestQueries := buildIstioRateQueries("istio_requests_total", labelVariants, hostFilter, "")
		latencyQueries := buildIstioLatencyQueries(labelVariants, hostFilter)
		status2xxQueries := buildIstioStatusQueries(labelVariants, hostFilter, "2xx")
		status4xxQueries := buildIstioStatusQueries(labelVariants, hostFilter, "4xx")
		status5xxQueries := buildIstioStatusQueries(labelVariants, hostFilter, "5xx")

		reqSamples, reqErr = queryPromRangeWithFallback(ctx, promURL, requestQueries, start, end, step)
		latencySamples, latencyErr = queryPromRangeWithFallback(ctx, promURL, latencyQueries, start, end, step)
		status2xxSamples, _ = queryPromRangeWithFallback(ctx, promURL, status2xxQueries, start, end, step)
		status4xxSamples, _ = queryPromRangeWithFallback(ctx, promURL, status4xxQueries, start, end, step)
		status5xxSamples, _ = queryPromRangeWithFallback(ctx, promURL, status5xxQueries, start, end, step)
	} else {
		cpuQuery := fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s", pod=~"%s-.*", container!=""}[5m]))`,
			namespace, serviceName)
		memoryQuery := fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s", pod=~"%s-.*", container!=""})`,
			namespace, serviceName)
		serviceFqdn := fmt.Sprintf("%s.%s.svc.cluster.local", serviceName, namespace)
		labelVariants := buildIstioLabelVariants(serviceName, namespace, serviceFqdn)
		requestQueries := buildIstioRateQueries("istio_requests_total", labelVariants, "", "")
		latencyQueries := buildIstioLatencyQueries(labelVariants, "")
		status2xxQueries := buildIstioStatusQueries(labelVariants, "", "2xx")
		status4xxQueries := buildIstioStatusQueries(labelVariants, "", "4xx")
		status5xxQueries := buildIstioStatusQueries(labelVariants, "", "5xx")

		cpuSamples, cpuErr = observability.QueryPrometheusRange(ctx, promURL, cpuQuery, start, end, step)
		memSamples, memErr = observability.QueryPrometheusRange(ctx, promURL, memoryQuery, start, end, step)
		reqSamples, reqErr = queryPromRangeWithFallback(ctx, promURL, requestQueries, start, end, step)
		latencySamples, latencyErr = queryPromRangeWithFallback(ctx, promURL, latencyQueries, start, end, step)
		status2xxSamples, _ = queryPromRangeWithFallback(ctx, promURL, status2xxQueries, start, end, step)
		status4xxSamples, _ = queryPromRangeWithFallback(ctx, promURL, status4xxQueries, start, end, step)
		status5xxSamples, _ = queryPromRangeWithFallback(ctx, promURL, status5xxQueries, start, end, step)
	}

	cpuValues := observability.FillSeries(cpuSamples, start, step, len(timestamps))
	memoryValues := observability.FillSeries(memSamples, start, step, len(timestamps))
	latencyValues := observability.FillSeries(latencySamples, start, step, len(timestamps))
	requestsValues := observability.FillSeries(reqSamples, start, step, len(timestamps))
	status2xxValues := observability.FillSeries(status2xxSamples, start, step, len(timestamps))
	status4xxValues := observability.FillSeries(status4xxSamples, start, step, len(timestamps))
	status5xxValues := observability.FillSeries(status5xxSamples, start, step, len(timestamps))

	for i, value := range cpuValues {
		cpuValues[i] = math.Round(value*100*10) / 10
	}
	for i, value := range memoryValues {
		memoryValues[i] = math.Round((value/(1024*1024*1024))*100*10) / 10
	}
	for i, value := range latencyValues {
		latencyValues[i] = math.Round(value*10) / 10
	}
	for i, value := range requestsValues {
		requestsValues[i] = math.Round((value*60)*10) / 10
	}
	for i, value := range status2xxValues {
		status2xxValues[i] = math.Round((value*60)*10) / 10
	}
	for i, value := range status4xxValues {
		status4xxValues[i] = math.Round((value*60)*10) / 10
	}
	for i, value := range status5xxValues {
		status5xxValues[i] = math.Round((value*60)*10) / 10
	}

	// Build diagnostics if any query failed or returned empty
	var diagnostics *observability.MetricsDiagnostics
	if cpuErr != nil || memErr != nil || latencyErr != nil || reqErr != nil ||
		len(cpuSamples) == 0 && len(memSamples) == 0 && len(latencySamples) == 0 && len(reqSamples) == 0 {
		errMsg := ""
		if cpuErr != nil {
			errMsg = cpuErr.Error()
		} else if memErr != nil {
			errMsg = memErr.Error()
		}
		diagnostics = &observability.MetricsDiagnostics{
			PrometheusURL:       promURL,
			Namespace:           metricsNamespace,
			ServiceName:         serviceName,
			TimeRangeStart:      start.Format(time.RFC3339),
			TimeRangeEnd:        end.Format(time.RFC3339),
			CpuQuerySuccess:     cpuErr == nil && len(cpuSamples) > 0,
			MemQuerySuccess:     memErr == nil && len(memSamples) > 0,
			LatencyQuerySuccess: latencyErr == nil && len(latencySamples) > 0,
			ReqQuerySuccess:     reqErr == nil && len(reqSamples) > 0,
			Error:               errMsg,
		}
	}

	response := observability.MetricsPayload{
		ServiceID:   serviceID,
		Environment: environment,
		Namespace:   metricsNamespace,
		Timestamps:  timestampValues,
		Cpu:         cpuValues,
		Memory:      memoryValues,
		LatencyP95:  latencyValues,
		Requests:    requestsValues,
		StatusCodes: &observability.StatusCodeSeries{
			TwoXX:  status2xxValues,
			FourXX: status4xxValues,
			FiveXX: status5xxValues,
		},
		Diagnostics: diagnostics,
	}
	c.JSON(http.StatusOK, response)
}

func buildStaticSiteHostFilter(ctx context.Context, service bson.M, serviceID, environment string) string {
	rules, err := shared.FindAll(ctx, shared.Collection(shared.RulesCollection), bson.M{
		"serviceId":   serviceID,
		"environment": environment,
	})
	if err != nil {
		return ""
	}
	hosts := make([]string, 0)
	for _, rule := range rules {
		for _, host := range shared.ToStringSlice(rule["hosts"]) {
			if normalized := normalizeHost(host); normalized != "" {
				hosts = append(hosts, normalized)
			}
		}
	}
	hosts = shared.UniqueStrings(hosts)
	if len(hosts) == 0 {
		if fallback := normalizeHost(shared.StringValue(service["url"])); fallback != "" {
			hosts = append(hosts, fallback)
		}
	}
	return buildHostRegexFilter(hosts)
}

func buildStaticHostFallbackFilter(serviceName string) string {
	if serviceName == "" {
		return ""
	}
	internalDomain := shared.EnvOrDefault("RELEASEA_INTERNAL_DOMAIN", "releasea.internal")
	externalDomain := shared.EnvOrDefault("RELEASEA_EXTERNAL_DOMAIN", "releasea.external")
	hosts := []string{
		fmt.Sprintf("%s.%s", serviceName, internalDomain),
		fmt.Sprintf("%s.%s", serviceName, externalDomain),
	}
	return buildHostRegexFilter(hosts)
}

func buildHostRegexFilter(hosts []string) string {
	if len(hosts) == 0 {
		return ""
	}
	escaped := make([]string, 0, len(hosts))
	for _, host := range hosts {
		trimmed := strings.TrimSpace(host)
		if trimmed == "" {
			continue
		}
		escaped = append(escaped, regexp.QuoteMeta(trimmed))
	}
	if len(escaped) == 0 {
		return ""
	}
	return fmt.Sprintf(`request_host=~"%s"`, strings.Join(escaped, "|"))
}

func normalizeHost(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "http://" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}

func buildIstioLabelVariants(workload, namespace, serviceFqdn string) []string {
	labels := make([]string, 0, 4)
	if workload != "" && namespace != "" {
		labels = append(labels,
			fmt.Sprintf(`destination_workload="%s", destination_workload_namespace="%s"`, workload, namespace),
			fmt.Sprintf(`destination_service_name="%s", destination_service_namespace="%s"`, workload, namespace),
			fmt.Sprintf(`destination_app="%s", destination_workload_namespace="%s"`, workload, namespace),
		)
	}
	if serviceFqdn != "" {
		labels = append(labels, fmt.Sprintf(`destination_service="%s"`, serviceFqdn))
	}
	return labels
}

func buildIstioRateQueries(metric string, labelVariants []string, hostFilter, extraLabel string) []string {
	out := make([]string, 0, len(labelVariants))
	for _, labels := range labelVariants {
		selector := labels
		if extraLabel != "" {
			selector = selector + ", " + extraLabel
		}
		if hostFilter != "" {
			selector = selector + ", " + hostFilter
		}
		out = append(out, fmt.Sprintf(`sum(rate(%s{%s}[5m]))`, metric, selector))
	}
	return out
}

func buildIstioLatencyQueries(labelVariants []string, hostFilter string) []string {
	out := make([]string, 0, len(labelVariants)*2)
	for _, labels := range labelVariants {
		selector := labels
		if hostFilter != "" {
			selector = selector + ", " + hostFilter
		}
		out = append(out, fmt.Sprintf(
			`histogram_quantile(0.95, sum(rate(istio_request_duration_milliseconds_bucket{%s}[5m])) by (le))`,
			selector,
		))
		out = append(out, fmt.Sprintf(
			`histogram_quantile(0.95, sum(rate(istio_request_duration_seconds_bucket{%s}[5m])) by (le)) * 1000`,
			selector,
		))
	}
	return out
}

func buildIstioStatusQueries(labelVariants []string, hostFilter, class string) []string {
	codeMatcher := ""
	classMatcher := ""
	switch class {
	case "2xx":
		codeMatcher = `response_code=~"2.."`
		classMatcher = `response_code_class="2xx"`
	case "4xx":
		codeMatcher = `response_code=~"4.."`
		classMatcher = `response_code_class="4xx"`
	case "5xx":
		codeMatcher = `response_code=~"5.."`
		classMatcher = `response_code_class="5xx"`
	default:
		return []string{}
	}
	queries := buildIstioRateQueries("istio_requests_total", labelVariants, hostFilter, codeMatcher)
	queries = append(queries, buildIstioRateQueries("istio_requests_total", labelVariants, hostFilter, classMatcher)...)
	return queries
}

func queryPromRangeWithFallback(ctx context.Context, promURL string, queries []string, start, end time.Time, step time.Duration) ([]observability.PromSample, error) {
	var lastErr error
	for _, query := range queries {
		if strings.TrimSpace(query) == "" {
			continue
		}
		samples, err := observability.QueryPrometheusRange(ctx, promURL, query, start, end, step)
		if err != nil {
			lastErr = err
			continue
		}
		if len(samples) > 0 {
			return samples, nil
		}
	}
	return nil, lastErr
}

func GetServiceLogs(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	environment := strings.TrimSpace(c.Query("environment"))
	if environment == "" {
		shared.RespondError(c, http.StatusBadRequest, "environment query parameter required")
		return
	}
	serviceName := shared.ToKubeName(shared.StringValue(service["name"]))
	if serviceName == "" {
		serviceName = shared.ToKubeName(serviceID)
	}
	if serviceName == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service name invalid")
		return
	}

	// Optional pod filter
	podFilter := strings.TrimSpace(c.Query("pod"))
	containerFilter := strings.TrimSpace(c.Query("container"))
	includeSidecars := strings.EqualFold(strings.TrimSpace(c.Query("includeSidecars")), "true") ||
		strings.TrimSpace(c.Query("includeSidecars")) == "1"

	from, to, _ := observability.ParseMetricsRange(c.Query("from"), c.Query("to"))
	limit := 200
	if value := strings.TrimSpace(c.Query("limit")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	namespace := shared.ResolveAppNamespace(environment)

	// Build query with optional pod/container filters
	var query string
	if podFilter != "" && containerFilter != "" {
		query = fmt.Sprintf(`{namespace="%s", pod="%s", container="%s"}`, namespace, podFilter, containerFilter)
	} else if podFilter != "" {
		if includeSidecars {
			query = fmt.Sprintf(`{namespace="%s", pod="%s"}`, namespace, podFilter)
		} else {
			query = fmt.Sprintf(`{namespace="%s", pod="%s", container!~"^istio-.*"}`, namespace, podFilter)
		}
	} else {
		if includeSidecars {
			query = fmt.Sprintf(`{namespace="%s", pod=~"%s-.*"}`, namespace, serviceName)
		} else {
			query = fmt.Sprintf(`{namespace="%s", pod=~"%s-.*", container!~"^istio-.*"}`, namespace, serviceName)
		}
	}

	log.Printf("[logs] service=%s env=%s namespace=%s query=%s from=%s to=%s limit=%d",
		serviceID, environment, namespace, query, from.Format(time.RFC3339), to.Format(time.RFC3339), limit)

	lokiURL := observability.LokiURL()
	logs, err := observability.QueryLokiRange(ctx, lokiURL, query, from, to, limit)
	if err != nil {
		log.Printf("[logs] service=%s env=%s error=%v", serviceID, environment, err)
		// Return empty logs with diagnostics instead of error
		c.JSON(http.StatusOK, gin.H{
			"logs": []observability.LogPayload{},
			"diagnostics": &observability.LogsDiagnostics{
				LokiURL:     lokiURL,
				Namespace:   namespace,
				ServiceName: serviceName,
				Query:       query,
				Error:       err.Error(),
			},
		})
		return
	}
	for i := range logs {
		logs[i].ServiceID = serviceID
	}
	c.JSON(http.StatusOK, logs)
}

// GetServicePods returns a list of pods for a service in a given environment
func GetServicePods(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	environment := strings.TrimSpace(c.Query("environment"))
	if environment == "" {
		shared.RespondError(c, http.StatusBadRequest, "environment query parameter required")
		return
	}
	serviceName := shared.ToKubeName(shared.StringValue(service["name"]))
	if serviceName == "" {
		serviceName = shared.ToKubeName(serviceID)
	}

	namespace := shared.ResolveAppNamespace(environment)

	// Query Loki for distinct pods in the last 3 hours
	now := time.Now().UTC()
	from := now.Add(-3 * time.Hour)
	query := fmt.Sprintf(`{namespace="%s", pod=~"%s-.*"}`, namespace, serviceName)

	logs, err := observability.QueryLokiRange(ctx, observability.LokiURL(), query, from, now, 500)
	if err != nil {
		// Return empty pod list with error info
		c.JSON(http.StatusOK, gin.H{
			"pods":      []string{},
			"namespace": namespace,
			"error":     err.Error(),
		})
		return
	}

	// Extract unique pod names
	podSet := make(map[string]struct{})
	for _, log := range logs {
		if podName, ok := log.Metadata["replicaName"].(string); ok && podName != "" {
			podSet[podName] = struct{}{}
		}
	}

	pods := make([]string, 0, len(podSet))
	for pod := range podSet {
		pods = append(pods, pod)
	}

	c.JSON(http.StatusOK, gin.H{
		"pods":        pods,
		"namespace":   namespace,
		"serviceName": serviceName,
	})
}

func GetServiceBuilds(c *gin.Context) {
	serviceID := c.Param("id")
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	builds, err := shared.FindAllSorted(ctx, shared.Collection(shared.BuildsCollection), bson.M{"serviceId": serviceID}, bson.M{"createdAt": -1})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load builds")
		return
	}
	c.JSON(http.StatusOK, builds)
}
