package workers

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	operations "releaseaapi/internal/features/operations/api"
	platformauth "releaseaapi/internal/platform/auth"
	operationqueue "releaseaapi/internal/platform/queue"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

// Workers

func GetWorkers(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.WorkersCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load workers")
		return
	}
	staleSeconds := getWorkerStaleSeconds()
	now := time.Now()
	for _, item := range items {
		lastHeartbeat := shared.StringValue(item["lastHeartbeat"])
		if lastHeartbeat == "" {
			item["status"] = "offline"
			item["isStale"] = true
			markRegistrationInactive(ctx, item, now)
			continue
		}
		parsed, err := time.Parse(time.RFC3339, lastHeartbeat)
		if err != nil {
			item["status"] = "offline"
			item["isStale"] = true
			markRegistrationInactive(ctx, item, now)
			continue
		}
		if now.Sub(parsed) > time.Duration(staleSeconds)*time.Second {
			item["status"] = "offline"
			item["isStale"] = true
			markRegistrationInactive(ctx, item, now)
		} else {
			item["isStale"] = false
		}
	}
	view := strings.ToLower(strings.TrimSpace(c.Query("view")))
	if view == "raw" {
		c.JSON(http.StatusOK, items)
		return
	}
	c.JSON(http.StatusOK, summarizeWorkers(items))
}

func GetWorkerBootstrapProfile(c *gin.Context) {
	// Return the effective runtime profile derived from process env to avoid
	// drift between persisted defaults and the live platform bootstrap config.
	c.JSON(http.StatusOK, shared.WorkerBootstrapProfileDocument(shared.NowISO()))
}

func UpdateWorker(c *gin.Context) {
	workerID := c.Param("id")
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	payload["updatedAt"] = shared.NowISO()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.WorkersCollection), workerID, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update worker")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.WorkersCollection), bson.M{"_id": workerID})
	c.JSON(http.StatusOK, updated)
}

func DeleteWorker(c *gin.Context) {
	workerID := c.Param("id")
	if strings.TrimSpace(workerID) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Worker ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	worker, err := shared.FindOne(ctx, shared.Collection(shared.WorkersCollection), bson.M{"id": workerID})
	if err != nil {
		worker, _ = shared.FindOne(ctx, shared.Collection(shared.WorkersCollection), bson.M{"_id": workerID})
	}
	credentialIDs := collectWorkerCredentialIDs(worker)

	deleteFilters := []bson.M{
		{"id": workerID},
		{"_id": workerID},
	}
	if len(credentialIDs) > 0 {
		deleteFilters = append(deleteFilters, bson.M{"credentialId": bson.M{"$in": credentialIDs}})
	}
	if _, err := shared.Collection(shared.WorkersCollection).DeleteMany(ctx, bson.M{"$or": deleteFilters}); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete worker")
		return
	}
	if len(credentialIDs) > 0 {
		if err := deleteWorkerRegistrations(ctx, credentialIDs); err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to delete worker registration")
			return
		}
	}
	c.Status(http.StatusNoContent)
}

func RestartWorker(c *gin.Context) {
	workerID := c.Param("id")
	if workerID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Worker ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	worker, err := shared.FindOne(ctx, shared.Collection(shared.WorkersCollection), bson.M{"id": workerID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Worker not found")
		return
	}

	deploymentName := shared.StringValue(worker["deploymentName"])
	if deploymentName == "" {
		deploymentName = shared.StringValue(worker["name"])
	}
	deploymentNamespace := shared.StringValue(worker["deploymentNamespace"])
	if deploymentNamespace == "" {
		deploymentNamespace = shared.StringValue(worker["namespace"])
	}
	if deploymentName == "" || deploymentNamespace == "" {
		shared.RespondError(c, http.StatusBadRequest, "Worker deployment metadata missing")
		return
	}

	activeOperation, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{
		"type":       operations.OperationTypeWorkerRestart,
		"resourceId": workerID,
		"status": bson.M{
			"$in": []string{operations.StatusQueued, operations.StatusInProgress},
		},
	})
	if err == nil {
		c.JSON(http.StatusAccepted, gin.H{"operation": activeOperation})
		return
	}
	if !errors.Is(err, mongo.ErrNoDocuments) && err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to check restart queue")
		return
	}

	opID := "op-" + uuid.NewString()
	now := shared.NowISO()
	opDoc := bson.M{
		"_id":          opID,
		"id":           opID,
		"type":         operations.OperationTypeWorkerRestart,
		"resourceType": "worker",
		"resourceId":   workerID,
		"status":       operations.StatusQueued,
		"createdAt":    now,
		"updatedAt":    now,
		"payload": bson.M{
			"deploymentName":      deploymentName,
			"deploymentNamespace": deploymentNamespace,
		},
		"requestedBy": shared.AuthDisplayName(c),
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.OperationsCollection), opDoc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to queue restart")
		return
	}
	if err := operationqueue.PublishOperation(ctx, opID); err != nil {
		_ = shared.UpdateByID(ctx, shared.Collection(shared.OperationsCollection), opID, bson.M{"status": operations.StatusFailed, "error": err.Error(), "updatedAt": shared.NowISO()})
		shared.RespondError(c, http.StatusServiceUnavailable, "Worker queue unavailable")
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"operation": opDoc})
}

func WorkerAuth(c *gin.Context) {
	regValue, ok := c.Get("authWorkerRegistration")
	if !ok {
		shared.RespondError(c, http.StatusUnauthorized, "Worker registration required")
		return
	}
	registration, ok := regValue.(bson.M)
	if !ok {
		shared.RespondError(c, http.StatusUnauthorized, "Worker registration invalid")
		return
	}
	if !isRegistrationActive(registration) {
		shared.RespondError(c, http.StatusUnauthorized, "Worker registration inactive")
		return
	}
	token, ttl, err := platformauth.GenerateWorkerAccessToken(registration)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to issue worker token")
		return
	}
	if regID := shared.StringValue(registration["id"]); regID != "" {
		now := shared.NowISO()
		_, _ = shared.Collection(shared.WorkerRegistrationsCollection).UpdateOne(
			context.Background(),
			bson.M{"id": regID},
			bson.M{"$set": bson.M{"status": "active", "lastUsedAt": now, "updatedAt": now}},
		)
	}
	c.JSON(http.StatusOK, gin.H{
		"accessToken": token,
		"expiresIn":   int(ttl.Seconds()),
	})
}

func getWorkerStaleSeconds() int {
	if value := strings.TrimSpace(os.Getenv("WORKER_STALE_SECONDS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			return parsed
		}
	}
	return 90
}

func markRegistrationInactive(ctx context.Context, worker bson.M, now time.Time) {
	registrationID := shared.StringValue(worker["credentialId"])
	if registrationID == "" {
		return
	}
	staleSeconds := getWorkerStaleSeconds()
	threshold := now.Add(-time.Duration(staleSeconds) * time.Second).UTC().Format(time.RFC3339)
	count, err := shared.Collection(shared.WorkersCollection).CountDocuments(ctx, bson.M{
		"credentialId": registrationID,
		"lastHeartbeat": bson.M{
			"$gte": threshold,
		},
	})
	if err == nil && count > 0 {
		return
	}
	_, _ = shared.Collection(shared.WorkerRegistrationsCollection).UpdateOne(
		ctx,
		bson.M{
			"_id":    registrationID,
			"status": bson.M{"$ne": "inactive"},
		},
		bson.M{
			"$set": bson.M{
				"status":         "inactive",
				"lastInactiveAt": now.UTC().Format(time.RFC3339),
				"updatedAt":      now.UTC().Format(time.RFC3339),
			},
		},
	)
}

func GetWorkerRegistrations(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.WorkerRegistrationsCollection), bson.M{
		"status": bson.M{"$ne": "revoked"},
	})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load worker registrations")
		return
	}
	for _, item := range items {
		delete(item, "token")
		delete(item, "tokenHash")
		delete(item, "expiresAt")
	}
	c.JSON(http.StatusOK, items)
}

func CreateWorkerRegistration(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if payload["id"] == nil {
		id := "wkr-reg-" + uuid.NewString()
		payload["id"] = id
		payload["_id"] = id
	}
	if payload["createdAt"] == nil {
		payload["createdAt"] = shared.NowISO()
	}
	if payload["status"] == nil {
		payload["status"] = "unused"
	}
	tokenValue := shared.StringValue(payload["token"])
	if tokenValue == "" {
		tokenValue = "frg_reg_" + uuid.NewString()
	}
	hashedToken, err := bcrypt.GenerateFromPassword([]byte(tokenValue), bcrypt.DefaultCost)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to secure token")
		return
	}
	tokenHintValue := shared.TokenHint(tokenValue)

	doc := bson.M{}
	for key, value := range payload {
		if key == "token" || key == "expiresAt" {
			continue
		}
		doc[key] = value
	}
	doc["tokenHash"] = string(hashedToken)
	doc["tokenHint"] = tokenHintValue

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.WorkerRegistrationsCollection), doc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create registration")
		return
	}
	delete(payload, "expiresAt")
	payload["token"] = tokenValue
	payload["tokenHint"] = tokenHintValue
	c.JSON(http.StatusOK, payload)
}

func DeleteWorkerRegistration(c *gin.Context) {
	registrationID := strings.TrimSpace(c.Param("id"))
	if registrationID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Worker registration ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	result, err := shared.Collection(shared.WorkerRegistrationsCollection).DeleteOne(
		ctx,
		bson.M{
			"$or": []bson.M{
				{"id": registrationID},
				{"_id": registrationID},
			},
		},
	)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete worker registration")
		return
	}
	if result.DeletedCount == 0 {
		shared.RespondError(c, http.StatusNotFound, "Worker registration not found")
		return
	}

	_, _ = shared.Collection(shared.WorkersCollection).DeleteMany(ctx, bson.M{
		"$or": []bson.M{
			{"credentialId": registrationID},
			{"credentialIds": registrationID},
		},
	})
	c.Status(http.StatusNoContent)
}

func Heartbeat(c *gin.Context) {
	var payload struct {
		ID                      string   `json:"id"`
		Name                    string   `json:"name"`
		Environment             string   `json:"environment"`
		Namespace               string   `json:"namespace"`
		NamespacePrefix         string   `json:"namespacePrefix"`
		Cluster                 string   `json:"cluster"`
		Version                 string   `json:"version"`
		BootstrapProfileVersion string   `json:"bootstrapProfileVersion"`
		Status                  string   `json:"status"`
		Tags                    []string `json:"tags"`
		DesiredAgents           int      `json:"desiredAgents"`
		DeploymentName          string   `json:"deploymentName"`
		DeploymentNamespace     string   `json:"deploymentNamespace"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if registrationValue, ok := c.Get("authWorkerRegistration"); ok {
		if registration, ok := registrationValue.(bson.M); ok {
			regID := shared.StringValue(registration["id"])
			if regID == "" {
				regID = shared.StringValue(registration["_id"])
			}
			if payload.ID == "" && regID != "" {
				payload.ID = regID
			}
			if regName := shared.StringValue(registration["name"]); regName != "" && payload.Name == "" {
				payload.Name = regName
			}
			if regEnv := shared.StringValue(registration["environment"]); regEnv != "" {
				payload.Environment = regEnv
			}
			if regNamespace := shared.StringValue(registration["namespace"]); regNamespace != "" && payload.Namespace == "" {
				payload.Namespace = regNamespace
			}
			if regPrefix := shared.StringValue(registration["namespacePrefix"]); regPrefix != "" {
				payload.NamespacePrefix = regPrefix
			}
			if regCluster := shared.StringValue(registration["cluster"]); regCluster != "" {
				payload.Cluster = regCluster
			}
			if regTags := shared.ToStringSlice(registration["tags"]); len(regTags) > 0 {
				payload.Tags = regTags
			}
		}
	}
	if payload.ID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Worker ID required")
		return
	}
	if payload.Name == "" {
		payload.Name = payload.ID
	}
	if payload.Status == "" {
		payload.Status = "online"
	}
	if payload.Cluster == "" {
		payload.Cluster = "k3d-local"
	}

	now := shared.NowISO()
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	workerDoc := bson.M{
		"_id":                     payload.ID,
		"id":                      payload.ID,
		"name":                    payload.Name,
		"environment":             payload.Environment,
		"namespace":               payload.Namespace,
		"namespacePrefix":         payload.NamespacePrefix,
		"cluster":                 payload.Cluster,
		"version":                 payload.Version,
		"bootstrapProfileVersion": payload.BootstrapProfileVersion,
		"status":                  payload.Status,
		"tags":                    payload.Tags,
		"lastHeartbeat":           now,
		"updatedAt":               now,
	}
	if payload.DeploymentName != "" {
		workerDoc["deploymentName"] = payload.DeploymentName
	}
	if payload.DeploymentNamespace != "" {
		workerDoc["deploymentNamespace"] = payload.DeploymentNamespace
	}

	existing, err := shared.FindOne(ctx, shared.Collection(shared.WorkersCollection), bson.M{"id": payload.ID})
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			workerDoc["createdAt"] = now
			workerDoc["registeredAt"] = now
			workerDoc["tasksCompleted"] = 0
			if payload.DesiredAgents > 0 {
				workerDoc["desiredAgents"] = payload.DesiredAgents
			} else {
				workerDoc["desiredAgents"] = 1
			}
			workerDoc["onlineAgents"] = 1
			if registrationValue, ok := c.Get("authWorkerRegistration"); ok {
				if registration, ok := registrationValue.(bson.M); ok {
					regID := shared.StringValue(registration["id"])
					if regID == "" {
						regID = shared.StringValue(registration["_id"])
					}
					workerDoc["credentialId"] = regID
				}
			} else {
				workerDoc["credentialId"] = ""
			}
			if err := shared.InsertOne(ctx, shared.Collection(shared.WorkersCollection), workerDoc); err != nil {
				shared.RespondError(c, http.StatusInternalServerError, "Failed to register worker")
				return
			}
			if registrationValue, ok := c.Get("authWorkerRegistration"); ok {
				if registration, ok := registrationValue.(bson.M); ok {
					regID := shared.StringValue(registration["id"])
					if regID != "" {
						_ = shared.UpdateByID(ctx, shared.Collection(shared.WorkerRegistrationsCollection), regID, bson.M{
							"status":     "active",
							"lastUsedAt": now,
							"workerId":   payload.ID,
							"updatedAt":  now,
						})
					}
				}
			}
			c.JSON(http.StatusOK, gin.H{"status": "registered"})
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load worker")
		return
	}

	existingDesired := shared.IntValue(existing["desiredAgents"])
	workerDoc["onlineAgents"] = shared.IntValue(existing["onlineAgents"])
	if workerDoc["onlineAgents"] == 0 {
		workerDoc["onlineAgents"] = 1
	}
	if payload.DesiredAgents > 0 {
		workerDoc["desiredAgents"] = payload.DesiredAgents
	} else if existingDesired > 0 {
		workerDoc["desiredAgents"] = existingDesired
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.WorkersCollection), payload.ID, workerDoc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update worker")
		return
	}
	if registrationValue, ok := c.Get("authWorkerRegistration"); ok {
		if registration, ok := registrationValue.(bson.M); ok {
			regID := shared.StringValue(registration["id"])
			if regID != "" {
				_ = shared.UpdateByID(ctx, shared.Collection(shared.WorkerRegistrationsCollection), regID, bson.M{
					"status":     "active",
					"lastUsedAt": now,
					"workerId":   payload.ID,
					"updatedAt":  now,
				})
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func summarizeWorkers(items []bson.M) []bson.M {
	groups := make(map[string]bson.M)
	heartbeats := make(map[string]time.Time)
	agentSets := make(map[string]map[string]struct{})
	agentStatuses := make(map[string]map[string]string)
	credentialSets := make(map[string]map[string]struct{})

	for _, item := range items {
		key := workerGroupKey(item)
		if key == "" {
			key = shared.StringValue(item["id"])
		}

		group, exists := groups[key]
		if !exists {
			name := shared.StringValue(item["name"])
			if name == "" {
				name = shared.StringValue(item["id"])
			}
			group = bson.M{
				"id":                      key,
				"primaryId":               shared.StringValue(item["id"]),
				"name":                    name,
				"environment":             shared.StringValue(item["environment"]),
				"namespace":               shared.StringValue(item["namespace"]),
				"namespacePrefix":         shared.StringValue(item["namespacePrefix"]),
				"cluster":                 shared.StringValue(item["cluster"]),
				"version":                 shared.StringValue(item["version"]),
				"bootstrapProfileVersion": shared.StringValue(item["bootstrapProfileVersion"]),
				"status":                  "offline",
				"tags":                    shared.ToStringSlice(item["tags"]),
				"lastHeartbeat":           shared.StringValue(item["lastHeartbeat"]),
				"tasksCompleted":          0,
				"registeredAt":            shared.StringValue(item["registeredAt"]),
				"desiredAgents":           0,
				"onlineAgents":            0,
				"credentialId":            "",
			}
			groups[key] = group
		}

		isStale, _ := item["isStale"].(bool)
		status := strings.ToLower(shared.StringValue(item["status"]))
		if status == "" {
			status = "offline"
		}
		if isStale {
			status = "offline"
		}

		agentID := shared.StringValue(item["id"])
		if agentID != "" {
			if agentSets[key] == nil {
				agentSets[key] = make(map[string]struct{})
			}
			agentSets[key][agentID] = struct{}{}
			if agentStatuses[key] == nil {
				agentStatuses[key] = make(map[string]string)
			}
			agentStatuses[key][agentID] = status
		}

		credentialID := shared.StringValue(item["credentialId"])
		if credentialID != "" {
			if credentialSets[key] == nil {
				credentialSets[key] = make(map[string]struct{})
			}
			credentialSets[key][credentialID] = struct{}{}
		}

		group["tasksCompleted"] = shared.IntValue(group["tasksCompleted"]) + shared.IntValue(item["tasksCompleted"])

		if regAt := shared.StringValue(item["registeredAt"]); regAt != "" {
			if group["registeredAt"] == "" {
				group["registeredAt"] = regAt
			} else if parsed, err := time.Parse(time.RFC3339, regAt); err == nil {
				if current, err := time.Parse(time.RFC3339, shared.StringValue(group["registeredAt"])); err == nil && parsed.Before(current) {
					group["registeredAt"] = regAt
				}
			}
		}

		if hb := shared.StringValue(item["lastHeartbeat"]); hb != "" {
			if parsed, err := time.Parse(time.RFC3339, hb); err == nil {
				if parsed.After(heartbeats[key]) {
					heartbeats[key] = parsed
					group["lastHeartbeat"] = hb
					group["primaryId"] = agentID
					group["name"] = shared.StringValue(item["name"])
					group["version"] = shared.StringValue(item["version"])
					group["bootstrapProfileVersion"] = shared.StringValue(item["bootstrapProfileVersion"])
					group["tags"] = shared.ToStringSlice(item["tags"])
					if credentialID != "" {
						group["credentialId"] = credentialID
					}
					if desired := shared.IntValue(item["desiredAgents"]); desired > 0 {
						group["desiredAgents"] = desired
					}
				}
			}
		}

		groups[key] = group
	}

	out := make([]bson.M, 0, len(groups))
	for key, group := range groups {
		agentIDs := make([]string, 0, len(agentSets[key]))
		for id := range agentSets[key] {
			agentIDs = append(agentIDs, id)
		}
		group["agentIds"] = agentIDs

		credentialIDs := make([]string, 0, len(credentialSets[key]))
		for id := range credentialSets[key] {
			credentialIDs = append(credentialIDs, id)
		}
		group["credentialIds"] = credentialIDs

		desiredAgents := shared.IntValue(group["desiredAgents"])
		if desiredAgents == 0 {
			desiredAgents = len(agentIDs)
		}
		onlineAgents := 0
		overallStatus := "offline"
		hasBusy := false
		hasOnline := false
		hasPending := false
		for _, status := range agentStatuses[key] {
			if status != "offline" {
				onlineAgents++
			}
			switch status {
			case "busy":
				hasBusy = true
			case "online":
				hasOnline = true
			case "pending":
				hasPending = true
			}
		}
		if hasBusy {
			overallStatus = "busy"
		} else if hasOnline {
			overallStatus = "online"
		} else if hasPending {
			overallStatus = "pending"
		}
		if desiredAgents < onlineAgents {
			desiredAgents = onlineAgents
		}
		group["desiredAgents"] = desiredAgents
		group["onlineAgents"] = onlineAgents
		group["status"] = overallStatus

		out = append(out, group)
	}

	return out
}

func workerGroupKey(item bson.M) string {
	if credentialID := shared.StringValue(item["credentialId"]); credentialID != "" {
		return "reg:" + credentialID
	}
	name := shared.StringValue(item["name"])
	if name == "" {
		name = shared.StringValue(item["id"])
	}
	environment := shared.StringValue(item["environment"])
	cluster := shared.StringValue(item["cluster"])
	return strings.Join([]string{name, environment, cluster}, "|")
}

func collectWorkerCredentialIDs(worker bson.M) []string {
	if worker == nil {
		return nil
	}
	credentialIDs := make([]string, 0, 2)
	if credentialID := shared.StringValue(worker["credentialId"]); credentialID != "" {
		credentialIDs = append(credentialIDs, credentialID)
	}
	credentialIDs = append(credentialIDs, shared.ToStringSlice(worker["credentialIds"])...)
	return shared.UniqueStrings(credentialIDs)
}

func deleteWorkerRegistrations(ctx context.Context, registrationIDs []string) error {
	if len(registrationIDs) == 0 {
		return nil
	}
	_, err := shared.Collection(shared.WorkerRegistrationsCollection).DeleteMany(
		ctx,
		bson.M{
			"$or": []bson.M{
				{"id": bson.M{"$in": registrationIDs}},
				{"_id": bson.M{"$in": registrationIDs}},
			},
		},
	)
	return err
}

func RegisterBuild(c *gin.Context) {
	if role, _ := c.Get("authRole"); role != "worker" {
		shared.RespondError(c, http.StatusForbidden, "Worker token required")
		return
	}

	var payload struct {
		ServiceID   string `json:"serviceId"`
		Commit      string `json:"commit"`
		ShortSha    string `json:"shortSha"`
		Tag         string `json:"tag"`
		Digest      string `json:"digest"`
		Environment string `json:"environment"`
		Image       string `json:"image"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.ServiceID == "" || payload.Tag == "" {
		shared.RespondError(c, http.StatusBadRequest, "serviceId and tag required")
		return
	}

	id := "build-" + uuid.NewString()
	now := shared.NowISO()
	doc := bson.M{
		"_id":         id,
		"id":          id,
		"serviceId":   payload.ServiceID,
		"commit":      payload.Commit,
		"shortSha":    payload.ShortSha,
		"tag":         payload.Tag,
		"digest":      payload.Digest,
		"environment": payload.Environment,
		"image":       payload.Image,
		"createdAt":   now,
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.BuildsCollection), doc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to register build")
		return
	}
	c.JSON(http.StatusCreated, doc)
}

func isRegistrationActive(registration bson.M) bool {
	status := strings.ToLower(shared.StringValue(registration["status"]))
	return status != "revoked"
}
