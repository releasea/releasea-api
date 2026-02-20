package deploys

import (
	"context"
	"log"
	"net/http"

	"releaseaapi/api/v1/operations"
	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func GetDeploys(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load deploys")
		return
	}
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

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	now := shared.NowISO()
	setUpdate := bson.M{
		"updatedAt": now,
	}
	if payload.Status != "" {
		nextStatus := operations.NormalizeDeployStatus(payload.Status)
		if !operations.IsKnownDeployStatus(nextStatus) {
			shared.RespondError(c, http.StatusBadRequest, "Invalid deploy status")
			return
		}
		currentDeploy, err := shared.FindOne(ctx, shared.Collection(shared.DeploysCollection), bson.M{"_id": deployID})
		if err != nil {
			shared.RespondError(c, http.StatusNotFound, "Deploy not found")
			return
		}
		currentStatus := operations.NormalizeDeployStatus(shared.StringValue(currentDeploy["status"]))
		if currentStatus == "" {
			currentStatus = operations.DeployStatusRequested
		}
		if !operations.CanTransitionDeployStatus(currentStatus, nextStatus) {
			shared.RespondError(c, http.StatusConflict, "Invalid deploy status transition")
			return
		}
		setUpdate["status"] = nextStatus
		if nextStatus == operations.DeployStatusCompleted || nextStatus == operations.DeployStatusFailed || nextStatus == operations.DeployStatusRollback {
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
	if len(lines) > 0 {
		update["$push"] = bson.M{
			"logs": bson.M{
				"$each": lines,
			},
		}
	}

	col := shared.Collection(shared.DeploysCollection)
	if _, err := col.UpdateOne(ctx, bson.M{"_id": deployID}, update); err != nil {
		log.Printf("[db] error during appendDeployLogs on %s: %v", col.Name(), err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to append deploy logs")
		return
	}
	c.Status(http.StatusNoContent)
}
