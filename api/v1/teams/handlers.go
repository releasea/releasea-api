package teams

import (
	"context"
	"net/http"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetTeams(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	teams, err := shared.FindAll(ctx, shared.Collection(shared.TeamsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load teams")
		return
	}
	c.JSON(http.StatusOK, teams)
}

func CreateTeam(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	id := "team-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	payload["createdAt"] = shared.NowISO()
	if _, ok := payload["members"]; !ok {
		payload["members"] = []interface{}{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.TeamsCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create team")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func UpdateTeam(c *gin.Context) {
	teamID := c.Param("id")
	if teamID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Team ID required")
		return
	}
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	payload["updatedAt"] = shared.NowISO()

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.TeamsCollection), teamID, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update team")
		return
	}
	updated, err := shared.FindOne(ctx, shared.Collection(shared.TeamsCollection), bson.M{"id": teamID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load team")
		return
	}
	c.JSON(http.StatusOK, updated)
}

func DeleteTeam(c *gin.Context) {
	teamID := c.Param("id")
	if teamID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Team ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.TeamsCollection), teamID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete team")
		return
	}
	c.Status(http.StatusNoContent)
}
