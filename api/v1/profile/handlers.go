package profile

import (
	"context"
	"net/http"
	"strings"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func GetProfile(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	profile, err := shared.FindOne(ctx, shared.Collection(shared.ProfileCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Profile not found")
		return
	}
	c.JSON(http.StatusOK, profile)
}

func UpdateProfile(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	profile, err := shared.FindOne(ctx, shared.Collection(shared.ProfileCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Profile not found")
		return
	}
	id, _ := profile["_id"].(string)
	if id == "" {
		shared.RespondError(c, http.StatusNotFound, "Profile not found")
		return
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.ProfileCollection), id, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update profile")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.ProfileCollection), bson.M{"_id": id})
	c.JSON(http.StatusOK, updated)
}

func ChangePassword(c *gin.Context) {
	var payload struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	payload.CurrentPassword = strings.TrimSpace(payload.CurrentPassword)
	payload.NewPassword = strings.TrimSpace(payload.NewPassword)
	if payload.CurrentPassword == "" || payload.NewPassword == "" {
		shared.RespondError(c, http.StatusBadRequest, "Current and new password required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	userID := authUserID(c)
	userFilter := bson.M{}
	if userID != "" {
		userFilter["id"] = userID
	}
	user, err := shared.FindOne(ctx, shared.Collection(shared.UsersCollection), userFilter)
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "User not found")
		return
	}
	userRecordID, _ := user["_id"].(string)
	if userRecordID == "" {
		shared.RespondError(c, http.StatusNotFound, "User not found")
		return
	}

	stored := shared.StringValue(user["password"])
	if !shared.VerifyPassword(payload.CurrentPassword, stored) {
		shared.RespondError(c, http.StatusForbidden, "Current password invalid")
		return
	}
	hashed, err := shared.HashPassword(payload.NewPassword)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to secure password")
		return
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.UsersCollection), userRecordID, bson.M{"password": hashed}); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update password")
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func RevokeSession(c *gin.Context) {
	sessionID := strings.TrimSpace(c.Param("id"))
	if sessionID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Session ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	profile, profileID := loadProfile(ctx, c)
	if profileID == "" {
		shared.RespondError(c, http.StatusNotFound, "Profile not found")
		return
	}

	sessions := toInterfaceSlice(profile["sessions"])
	next := make([]interface{}, 0, len(sessions))
	for _, session := range sessions {
		if sessionID == sessionStringID(session) {
			continue
		}
		next = append(next, session)
	}

	_ = shared.UpdateByID(ctx, shared.Collection(shared.ProfileCollection), profileID, bson.M{
		"sessions":  next,
		"updatedAt": shared.NowISO(),
	})
	c.Status(http.StatusNoContent)
}

func ConnectProvider(c *gin.Context) {
	providerID := strings.TrimSpace(c.Param("id"))
	if providerID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Provider ID required")
		return
	}

	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	profile, profileID := loadProfile(ctx, c)
	if profileID == "" {
		shared.RespondError(c, http.StatusNotFound, "Profile not found")
		return
	}

	connected := toInterfaceSlice(profile["connectedProviders"])
	next := make([]interface{}, 0, len(connected))
	updated := false
	now := shared.NowISO()
	entry := bson.M{
		"id":          providerID,
		"provider":    providerID,
		"connectedAt": now,
	}
	for key, value := range payload {
		if strings.TrimSpace(key) == "" {
			continue
		}
		entry[key] = value
	}

	for _, item := range connected {
		if providerID == sessionStringID(item) {
			next = append(next, entry)
			updated = true
			continue
		}
		next = append(next, item)
	}
	if !updated {
		next = append(next, entry)
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.ProfileCollection), profileID, bson.M{
		"connectedProviders": next,
		"updatedAt":          now,
	}); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to connect provider")
		return
	}
	profile["connectedProviders"] = next
	c.JSON(http.StatusOK, profile)
}

func DisconnectProvider(c *gin.Context) {
	providerID := strings.TrimSpace(c.Param("id"))
	if providerID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Provider ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	profile, profileID := loadProfile(ctx, c)
	if profileID == "" {
		shared.RespondError(c, http.StatusNotFound, "Profile not found")
		return
	}

	connected := toInterfaceSlice(profile["connectedProviders"])
	next := make([]interface{}, 0, len(connected))
	for _, item := range connected {
		if providerID == sessionStringID(item) {
			continue
		}
		next = append(next, item)
	}

	_ = shared.UpdateByID(ctx, shared.Collection(shared.ProfileCollection), profileID, bson.M{
		"connectedProviders": next,
		"updatedAt":          shared.NowISO(),
	})
	c.Status(http.StatusNoContent)
}

func DeleteProfile(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	profile, profileID := loadProfile(ctx, c)
	if profileID == "" {
		shared.RespondError(c, http.StatusNotFound, "Profile not found")
		return
	}
	userID := shared.StringValue(profile["id"])
	if userID == "" {
		userID = profileID
	}

	if userID != "" {
		_ = shared.DeleteByID(ctx, shared.Collection(shared.UsersCollection), userID)
		_, _ = shared.Collection(shared.TeamsCollection).UpdateMany(ctx, bson.M{}, bson.M{
			"$pull": bson.M{
				"members": bson.M{"id": userID},
			},
		})
	}
	_, _ = shared.Collection(shared.IdpSessionsCollection).DeleteMany(ctx, bson.M{"userId": userID})
	_ = shared.DeleteByID(ctx, shared.Collection(shared.ProfileCollection), profileID)
	c.Status(http.StatusNoContent)
}

func authUserID(c *gin.Context) string {
	if value, ok := c.Get("authUserId"); ok {
		if id, ok := value.(string); ok && id != "" {
			return id
		}
	}
	return ""
}

func loadProfile(ctx context.Context, c *gin.Context) (bson.M, string) {
	userID := authUserID(c)
	filter := bson.M{}
	if userID != "" {
		filter["id"] = userID
	}
	profile, err := shared.FindOne(ctx, shared.Collection(shared.ProfileCollection), filter)
	if err != nil {
		return nil, ""
	}
	id, _ := profile["_id"].(string)
	if id == "" {
		id = shared.StringValue(profile["id"])
	}
	return profile, id
}

func sessionStringID(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]interface{}:
		if id, ok := v["id"].(string); ok && id != "" {
			return id
		}
		if id, ok := v["provider"].(string); ok && id != "" {
			return id
		}
	}
	return ""
}

func toInterfaceSlice(value interface{}) []interface{} {
	if value == nil {
		return nil
	}
	switch v := value.(type) {
	case []interface{}:
		return v
	default:
		return nil
	}
}
