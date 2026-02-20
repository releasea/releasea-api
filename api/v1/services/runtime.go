package services

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/api/v1/operations"
	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

type runtimeUpdatePayload struct {
	Environment string `json:"environment"`
	Status      string `json:"status"`
	Reason      string `json:"reason"`
}

type blueGreenPrimaryPayload struct {
	Environment string `json:"environment"`
	ActiveSlot  string `json:"activeSlot"`
}

func UpdateServiceRuntime(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	var payload runtimeUpdatePayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	envKey := normalizeRuntimeEnvironment(payload.Environment)
	if envKey == "" {
		shared.RespondError(c, http.StatusBadRequest, "Environment required")
		return
	}

	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if !isValidRuntimeStatus(status) {
		shared.RespondError(c, http.StatusBadRequest, "Invalid runtime status")
		return
	}

	if _, ok := c.Get("authWorkerRegistration"); !ok {
		shared.RespondError(c, http.StatusForbidden, "Worker token required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	now := shared.NowISO()
	update := bson.M{
		"runtime." + envKey: bson.M{
			"status":    status,
			"reason":    strings.TrimSpace(payload.Reason),
			"updatedAt": now,
		},
		"updatedAt": now,
	}

	currentStatus := strings.ToLower(shared.StringValue(service["status"]))
	isActive := shared.BoolValue(service["isActive"])
	nextStatus := mapRuntimeToServiceStatus(status)

	if shouldUpdateServiceStatus(currentStatus, isActive, nextStatus) {
		update["status"] = nextStatus
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update runtime status")
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func UpdateBlueGreenPrimary(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	var payload blueGreenPrimaryPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	activeSlot := strings.ToLower(strings.TrimSpace(payload.ActiveSlot))
	if activeSlot != "blue" && activeSlot != "green" {
		shared.RespondError(c, http.StatusBadRequest, "Invalid active slot")
		return
	}

	if _, ok := c.Get("authWorkerRegistration"); !ok {
		shared.RespondError(c, http.StatusForbidden, "Worker token required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	strategy := shared.MapPayload(service["deploymentStrategy"])
	if strategy == nil {
		strategy = bson.M{}
	}
	strategyType := strings.ToLower(strings.TrimSpace(shared.StringValue(strategy["type"])))
	if strategyType != "blue-green" {
		shared.RespondError(c, http.StatusBadRequest, "Service is not using blue-green strategy")
		return
	}
	strategy["type"] = "blue-green"
	strategy["blueGreenPrimary"] = activeSlot

	now := shared.NowISO()
	update := bson.M{
		"deploymentStrategy": strategy,
		"updatedAt":          now,
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.ServicesCollection), serviceID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update blue-green strategy")
		return
	}
	_ = operations.RepublishRulesForServiceStrategy(ctx, serviceID)

	c.JSON(http.StatusOK, gin.H{
		"status":     "ok",
		"activeSlot": activeSlot,
	})
}

func isValidRuntimeStatus(value string) bool {
	switch value {
	case "healthy", "degraded", "crashloop", "pending", "error", "unknown", "idle":
		return true
	default:
		return false
	}
}

func normalizeRuntimeEnvironment(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "prod", "production", "live":
		return "prod"
	case "staging", "stage", "pre-prod", "preprod", "uat":
		return "staging"
	case "dev", "development", "qa", "sandbox", "test", "testing", "preview", "local":
		return "dev"
	default:
		return value
	}
}

func mapRuntimeToServiceStatus(value string) string {
	switch value {
	case "healthy":
		return "running"
	case "idle":
		return "idle"
	case "pending", "unknown":
		return "pending"
	case "degraded", "crashloop", "error":
		return "error"
	default:
		return ""
	}
}

func shouldUpdateServiceStatus(current string, isActive bool, next string) bool {
	if next == "" || current == "" {
		return false
	}
	if !isActive {
		return false
	}
	switch current {
	case "creating", "created":
		return false
	default:
		return current != next
	}
}
