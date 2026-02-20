package governance

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetGovernanceSettings(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	settings, err := shared.FindOne(ctx, shared.Collection(shared.GovernanceSettingsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Governance settings not found")
		return
	}
	c.JSON(http.StatusOK, settings)
}

func UpdateGovernanceSettings(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	settings, err := shared.FindOne(ctx, shared.Collection(shared.GovernanceSettingsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Governance settings not found")
		return
	}
	id, _ := settings["_id"].(string)
	if id == "" {
		shared.RespondError(c, http.StatusNotFound, "Governance settings not found")
		return
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.GovernanceSettingsCollection), id, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update governance settings")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.GovernanceSettingsCollection), bson.M{"_id": id})
	c.JSON(http.StatusOK, updated)
}

func GetGovernanceApprovals(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.GovernanceApprovalsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load approvals")
		return
	}
	c.JSON(http.StatusOK, items)
}

func CreateGovernanceApproval(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	id := "apr-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	payload["status"] = "pending"
	payload["requestedAt"] = shared.NowISO()
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.GovernanceApprovalsCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create approval")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func ReviewGovernanceApproval(c *gin.Context) {
	approvalID := c.Param("id")
	var payload struct {
		Status  string `json:"status"`
		Comment string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	update := bson.M{
		"status":        payload.Status,
		"reviewedAt":    shared.NowISO(),
		"reviewComment": payload.Comment,
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.GovernanceApprovalsCollection), approvalID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update approval")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func DeleteGovernanceApproval(c *gin.Context) {
	approvalID := c.Param("id")
	if strings.TrimSpace(approvalID) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Approval ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.GovernanceApprovalsCollection), approvalID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete approval")
		return
	}
	c.Status(http.StatusNoContent)
}

func GetGovernanceAudit(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.GovernanceAuditCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load audit logs")
		return
	}
	c.JSON(http.StatusOK, items)
}
