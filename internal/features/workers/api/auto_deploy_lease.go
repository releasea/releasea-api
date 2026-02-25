package workers

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	defaultAutoDeployLeaseTTLSeconds = 90
	minAutoDeployLeaseTTLSeconds     = 30
	maxAutoDeployLeaseTTLSeconds     = 10 * 60
)

func AcquireAutoDeployLease(c *gin.Context) {
	if role, _ := c.Get("authRole"); role != "worker" {
		shared.RespondError(c, http.StatusForbidden, "Worker token required")
		return
	}

	var payload struct {
		Holder      string `json:"holder"`
		Environment string `json:"environment"`
		TTLSeconds  int    `json:"ttlSeconds"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		shared.RespondError(c, http.StatusBadRequest, "Invalid payload")
		return
	}

	holder := strings.TrimSpace(payload.Holder)
	if holder == "" {
		holder = strings.TrimSpace(shared.AuthDisplayName(c))
	}
	if holder == "" {
		holder = "worker"
	}

	environment := strings.TrimSpace(payload.Environment)
	if environment == "" {
		if registrationValue, ok := c.Get("authWorkerRegistration"); ok {
			if registration, ok := registrationValue.(bson.M); ok {
				environment = shared.StringValue(registration["environment"])
			}
		}
	}
	environment = normalizeAutoDeployLeaseEnvironment(environment)
	ttlSeconds := normalizeAutoDeployLeaseTTL(payload.TTLSeconds)

	now := time.Now().UTC()
	nowISO := now.Format(time.RFC3339)
	expiresAt := now.Add(time.Duration(ttlSeconds) * time.Second).Format(time.RFC3339)
	leaseID := "autodeploy:" + environment

	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	col := shared.Collection(shared.WorkerLeasesCollection)
	filter := bson.M{
		"_id": leaseID,
		"$or": []bson.M{
			{"holder": holder},
			{"expiresAt": bson.M{"$exists": false}},
			{"expiresAt": bson.M{"$lte": nowISO}},
		},
	}
	update := bson.M{
		"$set": bson.M{
			"holder":      holder,
			"environment": environment,
			"ttlSeconds":  ttlSeconds,
			"updatedAt":   nowISO,
			"expiresAt":   expiresAt,
		},
	}

	result, err := col.UpdateOne(ctx, filter, update, options.Update().SetUpsert(false))
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to update auto-deploy lease")
		return
	}

	granted := result.MatchedCount > 0
	if !granted {
		leaseDoc := bson.M{
			"_id":         leaseID,
			"id":          leaseID,
			"type":        "auto-deploy",
			"holder":      holder,
			"environment": environment,
			"ttlSeconds":  ttlSeconds,
			"createdAt":   nowISO,
			"updatedAt":   nowISO,
			"expiresAt":   expiresAt,
		}
		if err := shared.InsertOne(ctx, col, leaseDoc); err == nil {
			granted = true
		} else if !isDuplicateKeyError(err) {
			shared.RespondError(c, http.StatusInternalServerError, "Failed to create auto-deploy lease")
			return
		}
	}

	leaseDoc, err := shared.FindOne(ctx, col, bson.M{"_id": leaseID})
	if errors.Is(err, mongo.ErrNoDocuments) {
		shared.RespondError(c, http.StatusInternalServerError, "Auto-deploy lease not found")
		return
	}
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load auto-deploy lease")
		return
	}

	currentHolder := strings.TrimSpace(shared.StringValue(leaseDoc["holder"]))
	currentExpires := strings.TrimSpace(shared.StringValue(leaseDoc["expiresAt"]))
	if currentHolder == "" {
		currentHolder = holder
	}
	if currentExpires == "" {
		currentExpires = expiresAt
	}

	validForHolder := currentHolder == holder && autoDeployLeaseStillValid(currentExpires, now)
	c.JSON(http.StatusOK, gin.H{
		"granted":     granted || validForHolder,
		"holder":      currentHolder,
		"environment": environment,
		"expiresAt":   currentExpires,
		"ttlSeconds":  ttlSeconds,
	})
}

func normalizeAutoDeployLeaseTTL(value int) int {
	if value <= 0 {
		value = defaultAutoDeployLeaseTTLSeconds
	}
	if value < minAutoDeployLeaseTTLSeconds {
		value = minAutoDeployLeaseTTLSeconds
	}
	if value > maxAutoDeployLeaseTTLSeconds {
		value = maxAutoDeployLeaseTTLSeconds
	}
	return value
}

func normalizeAutoDeployLeaseEnvironment(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "prod", "production", "live":
		return "prod"
	case "staging", "stage", "pre-prod", "preprod", "uat":
		return "staging"
	case "dev", "development", "qa", "sandbox", "test", "testing", "preview", "local":
		return "dev"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func autoDeployLeaseStillValid(expiresAt string, now time.Time) bool {
	if strings.TrimSpace(expiresAt) == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, expiresAt)
	if err != nil {
		return false
	}
	return parsed.After(now)
}

func isDuplicateKeyError(err error) bool {
	var writeErr mongo.WriteException
	if errors.As(err, &writeErr) {
		for _, item := range writeErr.WriteErrors {
			if item.Code == 11000 {
				return true
			}
		}
	}
	var commandErr mongo.CommandError
	if errors.As(err, &commandErr) && commandErr.Code == 11000 {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "e11000")
}
