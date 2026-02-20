package identity

import (
	"context"
	"net/http"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetIdpConfig(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	config, err := shared.FindOne(ctx, shared.Collection(shared.IdpConfigCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Identity provider config not found")
		return
	}
	c.JSON(http.StatusOK, config)
}

func UpdateIdpConfig(c *gin.Context) {
	var payload idpConfig
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	payload.normalize()
	if err := payload.validate(); err != nil {
		shared.RespondError(c, http.StatusBadRequest, err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	config, err := shared.FindOne(ctx, shared.Collection(shared.IdpConfigCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Identity provider config not found")
		return
	}
	id, _ := config["_id"].(string)
	if id == "" {
		shared.RespondError(c, http.StatusNotFound, "Identity provider config not found")
		return
	}
	document, err := payload.document()
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to encode identity config")
		return
	}
	if err := shared.UpdateByID(ctx, shared.Collection(shared.IdpConfigCollection), id, document); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update identity config")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.IdpConfigCollection), bson.M{"_id": id})
	c.JSON(http.StatusOK, updated)
}

func GetIdpConnections(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.IdpConnectionsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load connections")
		return
	}
	c.JSON(http.StatusOK, items)
}

func CreateIdpConnection(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	id := "idp-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	payload["createdAt"] = shared.NowISO()
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.IdpConnectionsCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create connection")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func DeleteIdpConnection(c *gin.Context) {
	id := c.Param("id")
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.IdpConnectionsCollection), id); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete connection")
		return
	}
	c.Status(http.StatusNoContent)
}

func GetGroupMappings(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.IdpMappingsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load mappings")
		return
	}
	c.JSON(http.StatusOK, items)
}

func CreateGroupMapping(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	id := "mapping-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.IdpMappingsCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create mapping")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func UpdateGroupMapping(c *gin.Context) {
	mappingID := c.Param("id")
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.UpdateByID(ctx, shared.Collection(shared.IdpMappingsCollection), mappingID, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update mapping")
		return
	}
	updated, _ := shared.FindOne(ctx, shared.Collection(shared.IdpMappingsCollection), bson.M{"_id": mappingID})
	c.JSON(http.StatusOK, updated)
}

func DeleteGroupMapping(c *gin.Context) {
	mappingID := c.Param("id")
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.DeleteByID(ctx, shared.Collection(shared.IdpMappingsCollection), mappingID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete mapping")
		return
	}
	c.Status(http.StatusNoContent)
}

func SyncGroupMappings(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func GetIdpSessions(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.IdpSessionsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load sessions")
		return
	}
	c.JSON(http.StatusOK, items)
}

func RevokeIdpSession(c *gin.Context) {
	id := c.Param("id")
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	_ = shared.DeleteByID(ctx, shared.Collection(shared.IdpSessionsCollection), id)
	c.Status(http.StatusNoContent)
}

func RevokeAllIdpSessions(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	_, _ = shared.Collection(shared.IdpSessionsCollection).DeleteMany(ctx, bson.M{})
	c.Status(http.StatusNoContent)
}

func GetIdpAudit(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	items, err := shared.FindAll(ctx, shared.Collection(shared.IdpAuditCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load audit logs")
		return
	}
	c.JSON(http.StatusOK, items)
}
