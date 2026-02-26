package environments

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	operations "releaseaapi/internal/features/operations/api"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

// CreateEnvironment creates a new dynamic environment. It is assigned to
// one of the three fixed namespaces based on the mapping rules.
func CreateEnvironment(c *gin.Context) {
	var payload struct {
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
		IsDefault   bool   `json:"isDefault"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		shared.RespondError(c, http.StatusBadRequest, "Environment name required")
		return
	}

	id := strings.TrimSpace(payload.ID)
	if id == "" {
		id = strings.ToLower(strings.ReplaceAll(name, " ", "-"))
	}
	if id == "" {
		id = "env-" + uuid.NewString()[:8]
	}

	namespace := shared.ResolveAppNamespace(id)

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	existing, _ := shared.FindOne(ctx, shared.Collection(shared.EnvironmentsCollection), bson.M{"id": id})
	if len(existing) > 0 {
		shared.RespondError(c, http.StatusConflict, fmt.Sprintf("Environment %q already exists", id))
		return
	}

	now := shared.NowISO()
	doc := bson.M{
		"_id":         id,
		"id":          id,
		"name":        name,
		"description": strings.TrimSpace(payload.Description),
		"color":       strings.TrimSpace(payload.Color),
		"isDefault":   payload.IsDefault,
		"namespace":   namespace,
		"createdAt":   now,
		"updatedAt":   now,
	}
	if err := shared.InsertOne(ctx, shared.Collection(shared.EnvironmentsCollection), doc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create environment")
		return
	}

	c.JSON(http.StatusOK, doc)
}

// UpdateEnvironment modifies an environment's metadata. If the rename would
// change the namespace mapping AND workloads exist in the old namespace, the
// operation is blocked (immutability lock).
func UpdateEnvironment(c *gin.Context) {
	envID := c.Param("id")
	if envID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Environment ID required")
		return
	}

	var payload struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Color       string `json:"color"`
		IsDefault   bool   `json:"isDefault"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	existing, err := shared.FindOne(ctx, shared.Collection(shared.EnvironmentsCollection), bson.M{"id": envID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Environment not found")
		return
	}

	oldNamespace := shared.ResolveAppNamespace(envID)
	_ = existing

	update := bson.M{
		"name":        strings.TrimSpace(payload.Name),
		"description": strings.TrimSpace(payload.Description),
		"color":       strings.TrimSpace(payload.Color),
		"isDefault":   payload.IsDefault,
		"namespace":   oldNamespace,
		"updatedAt":   shared.NowISO(),
	}

	if err := shared.UpdateByID(ctx, shared.Collection(shared.EnvironmentsCollection), envID, update); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update environment")
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "updated", "namespace": oldNamespace})
}

// DeleteEnvironment removes an environment. If workloads exist in the mapped
// namespace for this environment, the deletion is blocked.
func DeleteEnvironment(c *gin.Context) {
	envID := c.Param("id")
	if envID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Environment ID required")
		return
	}

	// Prevent deleting core environments
	switch envID {
	case "dev", "staging", "prod":
		shared.RespondError(c, http.StatusForbidden, fmt.Sprintf("Cannot delete core environment %q", envID))
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	// Check if workers/deploys reference this environment
	workerCount, _ := shared.Collection(shared.WorkersCollection).CountDocuments(ctx, bson.M{"environment": envID})
	if workerCount > 0 {
		shared.RespondError(c, http.StatusConflict,
			fmt.Sprintf("Cannot delete environment %q: %d worker(s) are still assigned to it", envID, workerCount))
		return
	}

	if err := shared.DeleteByID(ctx, shared.Collection(shared.EnvironmentsCollection), envID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete environment")
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}

// CheckEnvironmentLock returns whether an environment is "locked" because
// workloads exist in its namespace. Used by the frontend to disable editing.
func CheckEnvironmentLock(c *gin.Context) {
	envID := c.Param("id")
	if envID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Environment ID required")
		return
	}

	namespace := shared.ResolveAppNamespace(envID)

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	deployCount, _ := shared.Collection(shared.DeploysCollection).CountDocuments(ctx, bson.M{
		"environment": envID,
		"status": bson.M{
			"$in": append(operations.DeploySuccessfulStatuses(), operations.DeployNonTerminalStatuses()...),
		},
	})

	workerCount, _ := shared.Collection(shared.WorkersCollection).CountDocuments(ctx, bson.M{"environment": envID})

	locked := deployCount > 0 || workerCount > 0

	c.JSON(http.StatusOK, gin.H{
		"environmentId": envID,
		"namespace":     namespace,
		"locked":        locked,
		"deployCount":   deployCount,
		"workerCount":   workerCount,
		"reason": func() string {
			if !locked {
				return ""
			}
			return fmt.Sprintf("Environment has %d active deploy(s) and %d worker(s). Renaming is disabled to prevent namespace drift.", deployCount, workerCount)
		}(),
	})
}
