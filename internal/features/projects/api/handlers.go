package projects

import (
	"context"
	"net/http"

	"releaseaapi/internal/features/operations/api"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

func GetProjects(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	projects, err := shared.FindAll(ctx, shared.Collection(shared.ProjectsCollection), bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load projects")
		return
	}
	c.JSON(http.StatusOK, projects)
}

func CreateProject(c *gin.Context) {
	var payload bson.M
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	id := "proj-" + uuid.NewString()
	payload["_id"] = id
	payload["id"] = id
	payload["createdAt"] = shared.NowISO()
	payload["updatedAt"] = shared.NowISO()
	if _, ok := payload["services"]; !ok {
		payload["services"] = []interface{}{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()
	if err := shared.InsertOne(ctx, shared.Collection(shared.ProjectsCollection), payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create project")
		return
	}
	c.JSON(http.StatusOK, payload)
}

func UpdateProject(c *gin.Context) {
	projectID := c.Param("id")
	if projectID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Project ID required")
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
	if err := shared.UpdateByID(ctx, shared.Collection(shared.ProjectsCollection), projectID, payload); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update project")
		return
	}
	updated, err := shared.FindOne(ctx, shared.Collection(shared.ProjectsCollection), bson.M{"_id": projectID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load project")
		return
	}
	c.JSON(http.StatusOK, updated)
}

func DeleteProject(c *gin.Context) {
	projectID := c.Param("id")
	if projectID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Project ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	project, err := shared.FindOne(ctx, shared.Collection(shared.ProjectsCollection), bson.M{"id": projectID})
	if err != nil {
		shared.RespondError(c, http.StatusNotFound, "Project not found")
		return
	}

	services, err := shared.FindAll(ctx, shared.Collection(shared.ServicesCollection), bson.M{"projectId": projectID})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load project services")
		return
	}

	serviceIDs := make([]string, 0, len(services))
	for _, service := range services {
		if id := shared.StringValue(service["id"]); id != "" {
			serviceIDs = append(serviceIDs, id)
		}
	}

	if len(serviceIDs) > 0 {
		blockingDeploys, err := shared.Collection(shared.DeploysCollection).CountDocuments(ctx, bson.M{
			"serviceId": bson.M{"$in": serviceIDs},
			"status": bson.M{
				"$in": append(operations.DeploySuccessfulStatuses(), operations.DeployNonTerminalStatuses()...),
			},
		})
		if err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to validate project deploys")
			return
		}
		if blockingDeploys > 0 {
			shared.RespondError(c, http.StatusConflict, "Project has deployed services")
			return
		}

		blockingRules, err := shared.Collection(shared.RulesCollection).CountDocuments(ctx, bson.M{
			"serviceId": bson.M{"$in": serviceIDs},
			"status":    bson.M{"$in": []string{"published", "publishing", "unpublishing", "queued", "in-progress"}},
		})
		if err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to validate project rules")
			return
		}
		if blockingRules > 0 {
			shared.RespondError(c, http.StatusConflict, "Project has published routes")
			return
		}
	}

	if len(services) > 0 {
		if err := cleanupManagedGithubRepos(ctx, projectID, project, services); err != nil {
			shared.RespondError(c, http.StatusBadGateway, err.Error())
			return
		}
	}

	if len(serviceIDs) > 0 {
		_, _ = shared.Collection(shared.RulesCollection).DeleteMany(ctx, bson.M{"serviceId": bson.M{"$in": serviceIDs}})
		_, _ = shared.Collection(shared.DeploysCollection).DeleteMany(ctx, bson.M{"serviceId": bson.M{"$in": serviceIDs}})
		_, _ = shared.Collection(shared.LogsCollection).DeleteMany(ctx, bson.M{"serviceId": bson.M{"$in": serviceIDs}})
		_, _ = shared.Collection(shared.ServicesCollection).DeleteMany(ctx, bson.M{"projectId": projectID})
	}

	if err := shared.DeleteByID(ctx, shared.Collection(shared.ProjectsCollection), projectID); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to delete project")
		return
	}

	c.Status(http.StatusNoContent)
}
