package deploys

import (
	"context"
	"log"
	"net/http"
	"strings"

	operations "releaseaapi/internal/features/operations/api"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func GetDeploys(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load deploys")
		return
	}
	operations.NormalizeDeployDocuments(items)
	c.JSON(http.StatusOK, items)
}

func AppendDeployLogs(c *gin.Context) {
	deployID := c.Param("id")
	if deployID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Deploy ID required")
		return
	}
	var payload struct {
		Lines          []string               `json:"lines"`
		Line           string                 `json:"line"`
		LogBatchID     string                 `json:"logBatchId"`
		Status         string                 `json:"status"`
		StrategyStatus map[string]interface{} `json:"strategyStatus"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	lines := payload.Lines
	if payload.Line != "" {
		lines = append(lines, payload.Line)
	}
	if len(lines) == 0 && payload.StrategyStatus == nil && payload.Status == "" {
		shared.RespondError(c, http.StatusBadRequest, "No deploy updates provided")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()

	now := shared.NowISO()
	setUpdate := bson.M{
		"updatedAt": now,
	}
	updateFilter := bson.M{"_id": deployID}
	logBatchID := strings.TrimSpace(payload.LogBatchID)
	if logBatchID != "" && len(lines) > 0 {
		// A stable worker-generated batch ID makes a retried delivery exactly
		// once even when the first response is lost after MongoDB commits it.
		updateFilter["logBatchIds"] = bson.M{"$ne": logBatchID}
	}
	nextStatus := ""
	terminalStatus := false
	if payload.Status != "" {
		nextStatus = operations.NormalizeDeployStatus(payload.Status)
		if !operations.IsKnownDeployStatus(nextStatus) {
			shared.RespondError(c, http.StatusBadRequest, "Invalid deploy status")
			return
		}
		currentDeploy, err := shared.FindOne(ctx, shared.Collection(shared.DeploysCollection), bson.M{"_id": deployID})
		if err != nil {
			shared.RespondError(c, http.StatusNotFound, "Deploy not found")
			return
		}
		currentRawStatus := shared.StringValue(currentDeploy["status"])
		currentStatus := operations.NormalizeDeployStatus(currentRawStatus)
		if currentStatus == "" {
			currentStatus = operations.DeployStatusRequested
		}
		if !operations.CanTransitionDeployStatus(currentStatus, nextStatus) {
			shared.RespondError(c, http.StatusConflict, "Invalid deploy status transition")
			return
		}
		// Compare-and-set prevents a delayed worker update from moving a deploy
		// backwards after another transition has already won the race.
		updateFilter["status"] = currentRawStatus
		setUpdate["status"] = nextStatus
		if nextStatus == operations.DeployStatusCompleted || nextStatus == operations.DeployStatusFailed || nextStatus == operations.DeployStatusRolledBack {
			terminalStatus = true
			setUpdate["finishedAt"] = now
		}
		if payload.StrategyStatus == nil {
			setUpdate["strategyStatus.phase"] = nextStatus
			setUpdate["strategyStatus.updatedAt"] = now
		}
	}
	if payload.StrategyStatus != nil {
		if phase := shared.StringValue(payload.StrategyStatus["phase"]); phase == "" && payload.Status != "" {
			payload.StrategyStatus["phase"] = operations.NormalizeDeployStatus(payload.Status)
		}
		payload.StrategyStatus["updatedAt"] = now
		setUpdate["strategyStatus"] = payload.StrategyStatus
	}

	update := bson.M{
		"$set": setUpdate,
	}
	if terminalStatus {
		update["$unset"] = bson.M{"activeKey": ""}
	}
	if len(lines) > 0 {
		update["$push"] = bson.M{
			"logs": bson.M{
				"$each": lines,
			},
		}
		if logBatchID != "" {
			update["$addToSet"] = bson.M{"logBatchIds": logBatchID}
		}
	}

	col := shared.Collection(shared.DeploysCollection)
	result, err := col.UpdateOne(ctx, updateFilter, update)
	if err != nil {
		log.Printf("[db] error during appendDeployLogs on %s: %v", col.Name(), err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to append deploy logs")
		return
	}
	if result.MatchedCount == 0 {
		// A repeated delivery of the same status is idempotent. Any different
		// winner is a real conflict and must not receive stale logs or metadata.
		currentDeploy, findErr := shared.FindOne(ctx, col, bson.M{"_id": deployID})
		if findErr == nil {
			if logBatchID != "" && containsBSONString(currentDeploy["logBatchIds"], logBatchID) {
				c.Status(http.StatusNoContent)
				return
			}
			if nextStatus != "" && operations.NormalizeDeployStatus(shared.StringValue(currentDeploy["status"])) == nextStatus {
				c.Status(http.StatusNoContent)
				return
			}
		}
		shared.RespondError(c, http.StatusConflict, "Deploy status changed concurrently")
		return
	}
	c.Status(http.StatusNoContent)
}

func containsBSONString(raw interface{}, target string) bool {
	switch values := raw.(type) {
	case []string:
		for _, value := range values {
			if value == target {
				return true
			}
		}
	case bson.A:
		for _, value := range values {
			if text, ok := value.(string); ok && text == target {
				return true
			}
		}
	}
	return false
}
