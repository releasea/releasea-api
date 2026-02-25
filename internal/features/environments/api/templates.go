package environments

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetDeployTemplates(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.DeployTemplatesCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load deploy templates")
		return
	}
	c.JSON(http.StatusOK, items)
}

func GetDeployTemplate(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Deploy template ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	item, err := shared.FindOne(ctx, shared.Collection(shared.DeployTemplatesCollection), bson.M{"id": templateID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Deploy template not found")
		return
	}
	c.JSON(http.StatusOK, item)
}

func CreateDeployTemplate(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if strings.TrimSpace(shared.StringValue(payload["name"])) == "" {
		shared.RespondError(c, http.StatusBadRequest, "Deploy template name required")
		return
	}
	rawResources, ok := payload["resources"]
	if !ok {
		shared.RespondError(c, http.StatusBadRequest, "Deploy template resources required")
		return
	}
	if resources, ok := rawResources.([]interface{}); !ok || len(resources) == 0 {
		shared.RespondError(c, http.StatusBadRequest, "Deploy template resources invalid")
		return
	}

	id := "tpl-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	payload["createdAt"] = shared.NowISO()
	payload["updatedAt"] = shared.NowISO()

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.DeployTemplatesCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create deploy template")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func UpdateDeployTemplate(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Deploy template ID required")
		return
	}
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	if rawResources, ok := payload["resources"]; ok {
		if resources, ok := rawResources.([]interface{}); !ok || len(resources) == 0 {
			shared.RespondError(c, http.StatusBadRequest, "Deploy template resources invalid")
			return
		}
	}
	payload["updatedAt"] = shared.NowISO()

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.DeployTemplatesCollection), templateID, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update deploy template")
		return
	}
	updated, err := shared.FindOne(ctx, shared.Collection(shared.DeployTemplatesCollection), bson.M{"_id": templateID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load deploy template")
		return
	}
	c.JSON(http.StatusOK, updated)
}

func DeleteDeployTemplate(c *gin.Context) {
	templateID := c.Param("id")
	if templateID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Deploy template ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.DeployTemplatesCollection), templateID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete deploy template")
		return
	}
	c.Status(http.StatusNoContent)
}
