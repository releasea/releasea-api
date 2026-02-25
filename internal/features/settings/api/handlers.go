package settings

import (
	"context"
	"net/http"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetPlatformSettings(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	settings, err := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Settings not found")
		return
	}
	c.JSON(http.StatusOK, settings)
}

func UpdatePlatformSettings(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	settings, err := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Settings not found")
		return
	}
	id, _ := settings["_id"].(string)
	if id == "" {
		shared.RespondError(c, http.StatusNotFound, "Settings not found")
		return
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.PlatformSettingsCollection), id, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update settings")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{"_id": id})
	c.JSON(http.StatusOK, updated)
}

func GetRuntimeProfiles(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	profiles, err := shared.FindAll(ctx, shared.Collection(shared.RuntimeProfilesCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load runtime profiles")
		return
	}
	c.JSON(http.StatusOK, profiles)
}

func CreateRuntimeProfile(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	id := "rp-" + uuid.NewString()
	now := shared.NowISO()
	payload["_id"] = id
	payload["id"] = id
	payload["createdAt"] = now
	payload["updatedAt"] = now

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.RuntimeProfilesCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create runtime profile")
		return
	}
	c.JSON(http.StatusCreated, payload)
}

func UpdateRuntimeProfile(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		shared.RespondError(c, http.StatusBadRequest, "Profile ID required")
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
	if err := shared.UpdateByID(ctx, shared.Collection(shared.RuntimeProfilesCollection), id, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update runtime profile")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.RuntimeProfilesCollection), bson.M{"_id": id})
	c.JSON(http.StatusOK, updated)
}

func DeleteRuntimeProfile(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		shared.RespondError(c, http.StatusBadRequest, "Profile ID required")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.RuntimeProfilesCollection), id); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete runtime profile")
		return
	}
	c.Status(http.StatusNoContent)
}
