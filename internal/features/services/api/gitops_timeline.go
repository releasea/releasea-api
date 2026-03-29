package services

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

func GetServiceGitOpsTimeline(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	if _, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID}); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			shared.RespondError(c, http.StatusNotFound, "Service not found")
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service")
		return
	}

	items, err := shared.FindAll(ctx, shared.Collection(shared.PlatformAuditCollection), bson.M{
		"resourceType": "service",
		"resourceId":   serviceID,
		"action": bson.M{
			"$in": []string{
				"service.gitops_pr.create",
				"service.gitops_argocd_pr.create",
				"service.gitops_flux_pr.create",
				"service.gitops_drift.state_changed",
			},
		},
	})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load GitOps timeline")
		return
	}

	sort.Slice(items, func(i, j int) bool {
		return shared.StringValue(items[i]["createdAt"]) > shared.StringValue(items[j]["createdAt"])
	})
	c.JSON(http.StatusOK, items)
}

