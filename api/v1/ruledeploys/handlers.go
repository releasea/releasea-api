package ruledeploys

import (
	"context"
	"log"
	"net/http"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func GetRuleDeploys(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.RuleDeploysCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load rule deploys")
		return
	}
	if items == nil {
		items = []bson.M{}
	}
	c.JSON(http.StatusOK, items)
}

func AppendRuleDeployLogs(c *gin.Context) {
	ruleDeployID := c.Param("id")
	if ruleDeployID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Rule deploy ID required")
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
		"$push": bson.M{"logs": bson.M{"$each": lines}},
		"$set":  bson.M{"updatedAt": shared.NowISO()},
	}
	col := shared.Collection(shared.RuleDeploysCollection)
	if _, err := col.UpdateOne(ctx, bson.M{"_id": ruleDeployID}, update); err != nil {
		log.Printf("[db] error during appendRuleDeployLogs on %s: %v", col.Name(), err)
		shared.RespondError(c, http.StatusInternalServerError, "Failed to append rule deploy logs")
		return
	}
	c.Status(http.StatusNoContent)
}
