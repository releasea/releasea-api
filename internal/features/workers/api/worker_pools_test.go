package workers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestSummarizeWorkerPoolsAggregatesInventoryAndHealth(t *testing.T) {
	now := time.Now().UTC()
	workers := []bson.M{
		{
			"id":              "wkr-1",
			"environment":     "dev",
			"cluster":         "cluster-a",
			"namespacePrefix": "releasea-apps",
			"namespace":       "releasea-apps-dev",
			"tags":            []string{"build", "dev"},
			"status":          "online",
			"desiredAgents":   2,
			"onlineAgents":    1,
			"lastHeartbeat":   now.Add(-30 * time.Second).Format(time.RFC3339),
		},
		{
			"id":              "wkr-2",
			"environment":     "dev",
			"cluster":         "cluster-a",
			"namespacePrefix": "releasea-apps",
			"namespace":       "releasea-apps-dev",
			"tags":            []string{"dev", "build"},
			"status":          "busy",
			"desiredAgents":   1,
			"onlineAgents":    1,
			"lastHeartbeat":   now.Format(time.RFC3339),
		},
		{
			"id":              "wkr-3",
			"environment":     "dev",
			"cluster":         "cluster-a",
			"namespacePrefix": "releasea-apps",
			"namespace":       "releasea-apps-dev",
			"tags":            []string{"gpu"},
			"status":          "offline",
			"desiredAgents":   1,
			"onlineAgents":    0,
		},
	}
	registrations := []bson.M{
		{
			"id":              "reg-1",
			"environment":     "dev",
			"cluster":         "cluster-a",
			"namespacePrefix": "releasea-apps",
			"namespace":       "releasea-system",
			"tags":            []string{"build", "dev"},
			"status":          "unused",
		},
		{
			"id":              "reg-2",
			"environment":     "dev",
			"cluster":         "cluster-a",
			"namespacePrefix": "releasea-apps",
			"namespace":       "releasea-system",
			"tags":            []string{"build", "dev"},
			"status":          "active",
		},
		{
			"id":              "reg-3",
			"environment":     "staging",
			"cluster":         "cluster-b",
			"namespacePrefix": "releasea-apps",
			"namespace":       "releasea-system",
			"tags":            []string{"staging"},
			"status":          "unused",
		},
	}

	pools := summarizeWorkerPools(workers, registrations)
	if len(pools) != 3 {
		t.Fatalf("pool count = %d, want %d", len(pools), 3)
	}

	byID := make(map[string]bson.M, len(pools))
	for _, pool := range pools {
		byID[pool["id"].(string)] = pool
	}

	devBuild := byID["dev|cluster-a|releasea-apps|build,dev"]
	if got := devBuild["status"]; got != "busy" {
		t.Fatalf("dev build status = %v, want %q", got, "busy")
	}
	if got := devBuild["workerCount"]; got != 2 {
		t.Fatalf("dev build workerCount = %v, want %d", got, 2)
	}
	if got := devBuild["registrationCount"]; got != 2 {
		t.Fatalf("dev build registrationCount = %v, want %d", got, 2)
	}
	if got := devBuild["onlineWorkers"]; got != 1 {
		t.Fatalf("dev build onlineWorkers = %v, want %d", got, 1)
	}
	if got := devBuild["busyWorkers"]; got != 1 {
		t.Fatalf("dev build busyWorkers = %v, want %d", got, 1)
	}
	if got := devBuild["desiredAgents"]; got != 3 {
		t.Fatalf("dev build desiredAgents = %v, want %d", got, 3)
	}
	if got := devBuild["onlineAgents"]; got != 2 {
		t.Fatalf("dev build onlineAgents = %v, want %d", got, 2)
	}
	if got := devBuild["pendingRegistrations"]; got != 1 {
		t.Fatalf("dev build pendingRegistrations = %v, want %d", got, 1)
	}
	if got := devBuild["activeRegistrations"]; got != 1 {
		t.Fatalf("dev build activeRegistrations = %v, want %d", got, 1)
	}
	if got := devBuild["availableAgents"]; got != 1 {
		t.Fatalf("dev build availableAgents = %v, want %d", got, 1)
	}
	if got := devBuild["capacityState"]; got != "constrained" {
		t.Fatalf("dev build capacityState = %v, want %q", got, "constrained")
	}
	if got := devBuild["capacityScore"]; got != 63 {
		t.Fatalf("dev build capacityScore = %v, want %d", got, 63)
	}

	staging := byID["staging|cluster-b|releasea-apps|staging"]
	if got := staging["status"]; got != "pending" {
		t.Fatalf("staging status = %v, want %q", got, "pending")
	}
	if got := staging["workerCount"]; got != 0 {
		t.Fatalf("staging workerCount = %v, want %d", got, 0)
	}
	if got := staging["registrationCount"]; got != 1 {
		t.Fatalf("staging registrationCount = %v, want %d", got, 1)
	}
	if got := staging["capacityState"]; got != "bootstrap" {
		t.Fatalf("staging capacityState = %v, want %q", got, "bootstrap")
	}
}

func TestGetWorkerPoolsReturnsAggregatedPools(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previous := workerPoolsLoader
	workerPoolsLoader = func(context.Context) ([]bson.M, error) {
		return []bson.M{
			{
				"id":                    "dev|cluster-a|releasea-apps|build,dev",
				"status":                "online",
				"environment":           "dev",
				"cluster":               "cluster-a",
				"namespacePrefix":       "releasea-apps",
				"tags":                  []string{"build", "dev"},
				"namespaces":            []string{"releasea-apps-dev", "releasea-system"},
				"workerCount":           1,
				"onlineWorkers":         1,
				"busyWorkers":           0,
				"offlineWorkers":        0,
				"pendingWorkers":        0,
				"registrationCount":     1,
				"activeRegistrations":   1,
				"pendingRegistrations":  0,
				"inactiveRegistrations": 0,
				"desiredAgents":         2,
				"onlineAgents":          1,
				"availableAgents":       1,
				"capacityScore":         55,
				"capacityState":         "constrained",
				"lastHeartbeat":         time.Now().UTC().Format(time.RFC3339),
			},
		}, nil
	}
	defer func() {
		workerPoolsLoader = previous
	}()

	recorder := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/v1/workers/pools", GetWorkerPools)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/workers/pools", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	if len(response) != 1 {
		t.Fatalf("response length = %d, want %d", len(response), 1)
	}
	if got := response[0]["cluster"]; got != "cluster-a" {
		t.Fatalf("cluster = %v, want %q", got, "cluster-a")
	}
	if got := response[0]["desiredAgents"]; got != float64(2) {
		t.Fatalf("desiredAgents = %v, want %d", got, 2)
	}
	if got := response[0]["capacityState"]; got != "constrained" {
		t.Fatalf("capacityState = %v, want %q", got, "constrained")
	}
}
