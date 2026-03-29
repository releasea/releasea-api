package governance

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	governancemodels "releaseaapi/internal/features/governance/models"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var findGovernanceExceptions = func(ctx context.Context, filter bson.M) ([]bson.M, error) {
	return shared.FindAllSorted(ctx, shared.Collection(shared.GovernanceExceptionsCollection), filter, bson.D{{Key: "createdAt", Value: -1}})
}

var findGovernanceExceptionByID = func(ctx context.Context, exceptionID string) (bson.M, error) {
	return shared.FindOne(ctx, shared.Collection(shared.GovernanceExceptionsCollection), bson.M{"id": exceptionID})
}

var findServiceForGovernanceException = func(ctx context.Context, serviceID string) (bson.M, error) {
	return shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
}

var insertGovernanceException = func(ctx context.Context, doc bson.M) error {
	return shared.InsertOne(ctx, shared.Collection(shared.GovernanceExceptionsCollection), doc)
}

var updateGovernanceExceptionByID = func(ctx context.Context, exceptionID string, update bson.M) error {
	return shared.UpdateByID(ctx, shared.Collection(shared.GovernanceExceptionsCollection), exceptionID, update)
}

var recordGovernanceExceptionAudit = recordGovernanceAudit

type governanceTemporaryExceptionResponse struct {
	ID          string   `json:"id"`
	Policy      string   `json:"policy"`
	ServiceID   string   `json:"serviceId"`
	ServiceName string   `json:"serviceName"`
	Environment string   `json:"environment"`
	Codes       []string `json:"codes"`
	Reason      string   `json:"reason"`
	ExpiresAt   string   `json:"expiresAt"`
	Status      string   `json:"status"`
	CreatedAt   string   `json:"createdAt"`
	CreatedBy   bson.M   `json:"createdBy,omitempty"`
	RevokedAt   string   `json:"revokedAt,omitempty"`
	RevokedBy   bson.M   `json:"revokedBy,omitempty"`
}

func normalizeGovernanceTemporaryException(item bson.M) governanceTemporaryExceptionResponse {
	now := time.Now().UTC()
	response := governanceTemporaryExceptionResponse{
		ID:          strings.TrimSpace(shared.StringValue(item["id"])),
		Policy:      shared.NormalizeGovernanceExceptionPolicy(shared.StringValue(item["policy"])),
		ServiceID:   strings.TrimSpace(shared.StringValue(item["serviceId"])),
		ServiceName: strings.TrimSpace(shared.StringValue(item["serviceName"])),
		Environment: shared.NormalizeOperationEnvironment(shared.StringValue(item["environment"])),
		Codes:       shared.NormalizeGovernanceExceptionCodes(shared.ToStringSlice(item["codes"])),
		Reason:      strings.TrimSpace(shared.StringValue(item["reason"])),
		ExpiresAt:   strings.TrimSpace(shared.StringValue(item["expiresAt"])),
		Status:      shared.GovernanceTemporaryExceptionStatus(item, now),
		CreatedAt:   strings.TrimSpace(shared.StringValue(item["createdAt"])),
		RevokedAt:   strings.TrimSpace(shared.StringValue(item["revokedAt"])),
	}
	if createdBy := shared.MapPayload(item["createdBy"]); len(createdBy) > 0 {
		response.CreatedBy = createdBy
	}
	if revokedBy := shared.MapPayload(item["revokedBy"]); len(revokedBy) > 0 {
		response.RevokedBy = revokedBy
	}
	return response
}

func resolveGovernanceExceptionPerformedBy(c *gin.Context) bson.M {
	return bson.M{
		"id":    strings.TrimSpace(c.GetString("authUserId")),
		"name":  strings.TrimSpace(shared.AuthDisplayName(c)),
		"email": strings.TrimSpace(c.GetString("authEmail")),
	}
}

func GetGovernanceExceptions(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	items, err := findGovernanceExceptions(ctx, bson.M{})
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load temporary exceptions")
		return
	}

	response := make([]governanceTemporaryExceptionResponse, 0, len(items))
	for _, item := range items {
		response = append(response, normalizeGovernanceTemporaryException(item))
	}
	sort.Slice(response, func(i, j int) bool {
		statusRank := func(status string) int {
			switch status {
			case "active":
				return 0
			case "expired":
				return 1
			case "revoked":
				return 2
			default:
				return 3
			}
		}
		if rankDelta := statusRank(response[i].Status) - statusRank(response[j].Status); rankDelta != 0 {
			return rankDelta < 0
		}
		return response[i].CreatedAt > response[j].CreatedAt
	})

	c.JSON(http.StatusOK, response)
}

func CreateGovernanceException(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}

	var payload governancemodels.CreateTemporaryExceptionPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	serviceID := strings.TrimSpace(payload.ServiceID)
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	policy := shared.NormalizeGovernanceExceptionPolicy(payload.Policy)
	if policy == "" {
		shared.RespondError(c, http.StatusBadRequest, "Unsupported exception policy")
		return
	}

	environment := shared.NormalizeOperationEnvironment(payload.Environment)
	if environment == "" {
		shared.RespondError(c, http.StatusBadRequest, "Environment required")
		return
	}

	reason := strings.TrimSpace(payload.Reason)
	if reason == "" {
		shared.RespondError(c, http.StatusBadRequest, "Reason required")
		return
	}

	expiresAt, err := time.Parse(time.RFC3339, strings.TrimSpace(payload.ExpiresAt))
	if err != nil {
		shared.RespondError(c, http.StatusBadRequest, "ExpiresAt must be a valid RFC3339 timestamp")
		return
	}
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(time.Now().UTC()) {
		shared.RespondError(c, http.StatusBadRequest, "Temporary exceptions must expire in the future")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	service, err := findServiceForGovernanceException(ctx, serviceID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			shared.RespondError(c, http.StatusNotFound, "Service not found")
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service")
		return
	}

	now := shared.NowISO()
	exceptionID := "gexc-" + uuid.NewString()
	doc := bson.M{
		"_id":         exceptionID,
		"id":          exceptionID,
		"policy":      policy,
		"serviceId":   serviceID,
		"serviceName": strings.TrimSpace(shared.StringValue(service["name"])),
		"environment": environment,
		"codes":       shared.NormalizeGovernanceExceptionCodes(payload.Codes),
		"reason":      reason,
		"expiresAt":   expiresAt.Format(time.RFC3339),
		"createdAt":   now,
		"createdBy":   resolveGovernanceExceptionPerformedBy(c),
	}

	if err := insertGovernanceException(ctx, doc); err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to create temporary exception")
		return
	}

	recordGovernanceExceptionAudit(
		ctx,
		"governance.exception.created",
		"service",
		serviceID,
		strings.TrimSpace(shared.StringValue(service["name"])),
		resolveGovernanceExceptionPerformedBy(c),
		map[string]interface{}{
			"exceptionId": exceptionID,
			"policy":      policy,
			"environment": environment,
			"codes":       doc["codes"],
			"reason":      reason,
			"expiresAt":   doc["expiresAt"],
		},
	)

	c.JSON(http.StatusCreated, normalizeGovernanceTemporaryException(doc))
}

func RevokeGovernanceException(c *gin.Context) {
	if !requireAdminRequest(c) {
		return
	}

	exceptionID := strings.TrimSpace(c.Param("id"))
	if exceptionID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Exception ID required")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	existing, err := findGovernanceExceptionByID(ctx, exceptionID)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			shared.RespondError(c, http.StatusNotFound, "Temporary exception not found")
			return
		}
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load temporary exception")
		return
	}

	if strings.TrimSpace(shared.StringValue(existing["revokedAt"])) == "" {
		revokedAt := shared.NowISO()
		revokedBy := resolveGovernanceExceptionPerformedBy(c)
		if err := updateGovernanceExceptionByID(ctx, exceptionID, bson.M{
			"revokedAt": revokedAt,
			"revokedBy": revokedBy,
		}); err != nil {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to revoke temporary exception")
			return
		}
		existing["revokedAt"] = revokedAt
		existing["revokedBy"] = revokedBy

		recordGovernanceExceptionAudit(
			ctx,
			"governance.exception.revoked",
			"service",
			strings.TrimSpace(shared.StringValue(existing["serviceId"])),
			strings.TrimSpace(shared.StringValue(existing["serviceName"])),
			revokedBy,
			map[string]interface{}{
				"exceptionId": exceptionID,
				"policy":      shared.NormalizeGovernanceExceptionPolicy(shared.StringValue(existing["policy"])),
				"environment": shared.NormalizeOperationEnvironment(shared.StringValue(existing["environment"])),
				"codes":       shared.NormalizeGovernanceExceptionCodes(shared.ToStringSlice(existing["codes"])),
			},
		)
	}

	c.JSON(http.StatusOK, normalizeGovernanceTemporaryException(existing))
}
