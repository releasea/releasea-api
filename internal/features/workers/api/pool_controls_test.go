package workers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestSetWorkerPoolMaintenanceRequiresReasonWhenEnabling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/api/v1/workers/pools/:id/maintenance", SetWorkerPoolMaintenance)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/workers/pools/prod%7Ccluster-a%7Creleasea-apps%7Cprod/maintenance", strings.NewReader(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestSetWorkerPoolDrainRequiresReasonWhenEnabling(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	router.POST("/api/v1/workers/pools/:id/drain", SetWorkerPoolDrain)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/workers/pools/prod%7Ccluster-a%7Creleasea-apps%7Cprod/drain", strings.NewReader(`{"enabled":true}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestSetWorkerPoolDrainRejectsMaintenancePool(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFind := findWorkerPoolControl
	findWorkerPoolControl = func(context.Context, string) (bson.M, error) {
		return bson.M{"maintenanceEnabled": true}, nil
	}
	defer func() {
		findWorkerPoolControl = previousFind
	}()

	router := gin.New()
	router.POST("/api/v1/workers/pools/:id/drain", SetWorkerPoolDrain)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/workers/pools/prod%7Ccluster-a%7Creleasea-apps%7Cprod/drain", strings.NewReader(`{"enabled":true,"reason":"node rollout"}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestGetCurrentWorkerPoolControlReturnsDefaultsWithoutControl(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previousFind := findWorkerPoolControl
	findWorkerPoolControl = func(context.Context, string) (bson.M, error) {
		return nil, context.Canceled
	}
	defer func() {
		findWorkerPoolControl = previousFind
	}()

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("authWorkerRegistration", bson.M{
			"environment":     "prod",
			"cluster":         "cluster-a",
			"namespacePrefix": "releasea-apps",
			"tags":            []string{"prod"},
		})
		c.Next()
	})
	router.GET("/api/v1/workers/pool-control", GetCurrentWorkerPoolControl)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/workers/pool-control", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	if got := response["id"]; got != "prod|cluster-a|releasea-apps|prod" {
		t.Fatalf("id = %v, want %q", got, "prod|cluster-a|releasea-apps|prod")
	}
	if got := response["drainEnabled"]; got != false {
		t.Fatalf("drainEnabled = %v, want false", got)
	}
}
