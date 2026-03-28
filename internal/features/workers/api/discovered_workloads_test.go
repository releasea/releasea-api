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

func TestGetDiscoveredWorkloadsAggregatesFreshWorkers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	previous := discoveredWorkloadsLoader
	discoveredWorkloadsLoader = func(context.Context) ([]bson.M, error) {
		now := time.Now().UTC().Format(time.RFC3339)
		stale := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339)
		return []bson.M{
			{
				"id":            "wkr-1",
				"name":          "Development Worker",
				"environment":   "dev",
				"cluster":       "cluster-a",
				"status":        "online",
				"lastHeartbeat": now,
				"discoveredWorkloads": []interface{}{
					bson.M{
						"name":      "payments",
						"kind":      "Deployment",
						"namespace": "releasea-apps-development",
						"containers": []interface{}{
							bson.M{"name": "payments", "image": "ghcr.io/acme/payments:1.2.3", "ports": []interface{}{8080}, "imported": true},
							bson.M{"name": "metrics-sidecar", "image": "ghcr.io/acme/metrics:0.4.0", "ports": []interface{}{9090}},
						},
						"serviceHints": []interface{}{
							bson.M{"name": "payments", "type": "ClusterIP", "ports": []interface{}{80, 8080}},
						},
						"ingressHints": []interface{}{
							bson.M{"name": "payments", "serviceNames": []interface{}{"payments"}, "hosts": []interface{}{"payments.example.com"}, "paths": []interface{}{"/"}, "tls": true},
						},
						"images":          []interface{}{"ghcr.io/acme/payments:1.2.3"},
						"primaryImage":    "ghcr.io/acme/payments:1.2.3",
						"ports":           []interface{}{8080, 9090},
						"port":            8080,
						"replicas":        2,
						"healthCheckPath": "/healthz",
						"probes": []interface{}{
							bson.M{"type": "readiness", "handler": "httpGet", "containerName": "payments", "path": "/healthz", "port": "8080"},
							bson.M{"type": "liveness", "handler": "tcpSocket", "containerName": "payments", "port": "8080"},
						},
						"environmentVariables": []interface{}{
							bson.M{"key": "PAYMENTS_MODE", "value": "cluster", "sourceType": "plain", "importable": true},
							bson.M{"key": "DB_PASSWORD", "sourceType": "secretKeyRef", "reference": "secret:payments#password", "importable": false},
						},
						"command":  []interface{}{"/bin/payments"},
						"args":     []interface{}{"serve", "--port", "8080"},
						"cpuMilli": 500,
						"memoryMi": 512,
					},
					bson.M{
						"name":         "nightly-sync",
						"kind":         "CronJob",
						"namespace":    "releasea-apps-development",
						"scheduleCron": "0 1 * * *",
					},
				},
			},
			{
				"id":            "wkr-2",
				"name":          "Development Worker 2",
				"environment":   "dev",
				"cluster":       "cluster-a",
				"status":        "busy",
				"lastHeartbeat": now,
				"discoveredWorkloads": []interface{}{
					bson.M{
						"name":      "payments",
						"kind":      "Deployment",
						"namespace": "releasea-apps-development",
					},
					bson.M{
						"name":         "orders",
						"kind":         "StatefulSet",
						"namespace":    "releasea-apps-development",
						"images":       []interface{}{"ghcr.io/acme/orders:3.4.5"},
						"primaryImage": "ghcr.io/acme/orders:3.4.5",
						"ports":        []interface{}{5432},
						"port":         5432,
						"replicas":     1,
					},
				},
			},
			{
				"id":            "wkr-3",
				"name":          "Staging Worker",
				"environment":   "staging",
				"cluster":       "cluster-a",
				"status":        "online",
				"lastHeartbeat": now,
				"discoveredWorkloads": []interface{}{
					bson.M{
						"name":      "catalog",
						"kind":      "Deployment",
						"namespace": "releasea-apps-staging",
					},
				},
			},
			{
				"id":            "wkr-4",
				"name":          "Old Worker",
				"environment":   "dev",
				"cluster":       "cluster-a",
				"status":        "online",
				"lastHeartbeat": stale,
				"discoveredWorkloads": []interface{}{
					bson.M{
						"name":      "legacy",
						"kind":      "Deployment",
						"namespace": "releasea-apps-development",
					},
				},
			},
			{
				"id":            "wkr-5",
				"name":          "Offline Worker",
				"environment":   "dev",
				"cluster":       "cluster-a",
				"status":        "offline",
				"lastHeartbeat": now,
				"discoveredWorkloads": []interface{}{
					bson.M{
						"name":      "ignored",
						"kind":      "Deployment",
						"namespace": "releasea-apps-development",
					},
				},
			},
		}, nil
	}
	defer func() {
		discoveredWorkloadsLoader = previous
	}()

	recorder := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/v1/workers/discovered-workloads", GetDiscoveredWorkloads)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/workers/discovered-workloads?environment=dev", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var response []map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}

	if len(response) != 3 {
		t.Fatalf("response length = %d, want %d", len(response), 3)
	}

	if got := response[0]["name"]; got != "nightly-sync" {
		t.Fatalf("first workload name = %v, want %q", got, "nightly-sync")
	}
	if got := response[0]["templateKind"]; got != "scheduled-job" {
		t.Fatalf("first workload templateKind = %v, want %q", got, "scheduled-job")
	}
	if got := response[1]["name"]; got != "payments" {
		t.Fatalf("second workload name = %v, want %q", got, "payments")
	}
	if got := response[1]["workerId"]; got != "wkr-1" {
		t.Fatalf("payments workerId = %v, want %q", got, "wkr-1")
	}
	containers, ok := response[1]["containers"].([]interface{})
	if !ok || len(containers) != 2 {
		t.Fatalf("payments containers = %#v, want two entries", response[1]["containers"])
	}
	probes, ok := response[1]["probes"].([]interface{})
	if !ok || len(probes) != 2 {
		t.Fatalf("payments probes = %#v, want two entries", response[1]["probes"])
	}
	serviceHints, ok := response[1]["serviceHints"].([]interface{})
	if !ok || len(serviceHints) != 1 {
		t.Fatalf("payments serviceHints = %#v, want one entry", response[1]["serviceHints"])
	}
	ingressHints, ok := response[1]["ingressHints"].([]interface{})
	if !ok || len(ingressHints) != 1 {
		t.Fatalf("payments ingressHints = %#v, want one entry", response[1]["ingressHints"])
	}
	paymentsEnv, ok := response[1]["environmentVariables"].([]interface{})
	if !ok || len(paymentsEnv) != 2 {
		t.Fatalf("payments environmentVariables = %#v, want two entries", response[1]["environmentVariables"])
	}
	secondEnv, ok := paymentsEnv[1].(map[string]any)
	if !ok || secondEnv["sourceType"] != "secretKeyRef" {
		t.Fatalf("second env var = %#v, want secretKeyRef entry", paymentsEnv[1])
	}
	if got := response[1]["cpuMilli"]; got != float64(500) {
		t.Fatalf("payments cpuMilli = %v, want %v", got, 500)
	}
	if got := response[1]["memoryMi"]; got != float64(512) {
		t.Fatalf("payments memoryMi = %v, want %v", got, 512)
	}
	if got := response[2]["name"]; got != "orders" {
		t.Fatalf("third workload name = %v, want %q", got, "orders")
	}
	if got := response[2]["port"]; got != float64(5432) {
		t.Fatalf("orders port = %v, want %v", got, 5432)
	}
}
