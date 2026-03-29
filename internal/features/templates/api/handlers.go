package templates

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func ListTemplates(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.ServiceTemplatesCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load templates")
		return
	}
	c.JSON(http.StatusOK, enrichTemplateDocuments(items))
}

func GetTemplate(c *gin.Context) {
	templateID := strings.TrimSpace(c.Param("id"))
	if templateID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Template ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	item, err := shared.FindOne(ctx, shared.Collection(shared.ServiceTemplatesCollection), bson.M{"_id": templateID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Template not found")
		return
	}
	c.JSON(http.StatusOK, enrichTemplateDocument(item))
}

func CreateTemplate(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid template payload")
		return
	}
	normalized, err := normalizeTemplatePayload(payload, true)
	if err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}

	id := strings.TrimSpace(shared.StringValue(normalized["id"]))
	if id == "" {
		id = "tpl-" + uuid.NewString()
	}
	normalized["_id"] = id
	normalized["id"] = id

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.ServiceTemplatesCollection), normalized); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create template")
		return
	}
	c.JSON(http.StatusOK, enrichTemplateDocument(normalized))
}

func UpdateTemplate(c *gin.Context) {
	templateID := strings.TrimSpace(c.Param("id"))
	if templateID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Template ID required")
		return
	}
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid template payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	existing, err := shared.FindOne(ctx, shared.Collection(shared.ServiceTemplatesCollection), bson.M{"_id": templateID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Template not found")
		return
	}
	merged := bson.M{}
	for key, value := range existing {
		merged[key] = value
	}
	for key, value := range payload {
		if key == "_id" || key == "id" || key == "createdAt" {
			continue
		}
		merged[key] = value
	}
	normalized, err := normalizeTemplatePayload(merged, false)
	if err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	normalized["_id"] = templateID
	normalized["id"] = templateID

	if err := shared.UpdateByID(ctx, shared.Collection(shared.ServiceTemplatesCollection), templateID, normalized); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update template")
		return
	}
	updated, err := shared.FindOne(ctx, shared.Collection(shared.ServiceTemplatesCollection), bson.M{"_id": templateID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load template")
		return
	}
	c.JSON(http.StatusOK, enrichTemplateDocument(updated))
}

func VerifyTemplates(c *gin.Context) {
	var payload interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid template payload")
		return
	}

	rawTemplates := make([]interface{}, 0, 4)
	switch typed := payload.(type) {
	case []interface{}:
		rawTemplates = typed
	case map[string]interface{}:
		rawTemplates = append(rawTemplates, typed)
	default:
		shared.RespondError(c, http.StatusBadRequest, "Template payload must be an object or array")
		return
	}

	verified := make([]bson.M, 0, len(rawTemplates))
	for _, raw := range rawTemplates {
		template := shared.MapPayload(raw)
		if len(template) == 0 {
			continue
		}
		verified = append(verified, verifyTemplateCandidate(template))
	}

	c.JSON(http.StatusOK, verified)
}

func DeleteTemplate(c *gin.Context) {
	templateID := strings.TrimSpace(c.Param("id"))
	if templateID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Template ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.ServiceTemplatesCollection), templateID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete template")
		return
	}
	c.Status(http.StatusNoContent)
}
