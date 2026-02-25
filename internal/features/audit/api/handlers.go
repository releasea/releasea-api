package audit

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func GetAuditEvents(c *gin.Context) {
	filter := bson.M{}
	if action := strings.TrimSpace(c.Query("action")); action != "" {
		filter["action"] = action
	}
	if resourceType := strings.TrimSpace(c.Query("resourceType")); resourceType != "" {
		filter["resourceType"] = resourceType
	}
	if resourceID := strings.TrimSpace(c.Query("resourceId")); resourceID != "" {
		filter["resourceId"] = resourceID
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		filter["status"] = status
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.PlatformAuditCollection), filter)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load audit logs")
		return
	}
	c.JSON(http.StatusOK, items)
}
