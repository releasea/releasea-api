package observability

import (
	"context"
	"net/http"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func GetLogs(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.LogsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load logs")
		return
	}
	c.JSON(http.StatusOK, items)
}
