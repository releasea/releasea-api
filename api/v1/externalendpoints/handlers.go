package externalendpoints

import (
	"context"
	"net/http"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetExternalEndpoints(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.ExternalEndpointsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load external endpoints")
		return
	}
	c.JSON(http.StatusOK, items)
}

func CreateExternalEndpoint(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	id := "external-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	payload["createdAt"] = shared.NowISO()
	payload["updatedAt"] = shared.NowISO()
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.ExternalEndpointsCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create endpoint")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func UpdateExternalEndpoint(c *gin.Context) {
	endpointID := c.Param("id")
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	payload["updatedAt"] = shared.NowISO()
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.ExternalEndpointsCollection), endpointID, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update endpoint")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.ExternalEndpointsCollection), bson.M{"_id": endpointID})
	c.JSON(http.StatusOK, updated)
}

func DeleteExternalEndpoint(c *gin.Context) {
	endpointID := c.Param("id")
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.ExternalEndpointsCollection), endpointID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete endpoint")
		return
	}
	c.Status(http.StatusNoContent)
}
