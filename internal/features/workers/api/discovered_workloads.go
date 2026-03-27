package workers

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

var discoveredWorkloadsLoader = func(ctx context.Context) ([]bson.M, error) {
	return shared.FindAll(ctx, shared.Collection(shared.WorkersCollection), bson.M{})
}

func GetDiscoveredWorkloads(c *gin.Context) {
	environmentFilter := strings.TrimSpace(c.Query("environment"))
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	items, err := discoveredWorkloadsLoader(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load discovered workloads")
		return
	}

	heartbeatThreshold := time.Now().UTC().Add(-time.Duration(getWorkerStaleSeconds()) * time.Second)
	seen := make(map[string]struct{})
	workloads := make([]bson.M, 0, 32)

	for _, worker := range items {
		status := strings.ToLower(strings.TrimSpace(shared.StringValue(worker["status"])))
		if status != "online" && status != "busy" && status != "pending" {
			continue
		}
		if !workerHeartbeatFresh(worker, heartbeatThreshold) {
			continue
		}

		workerEnvironment := strings.TrimSpace(shared.StringValue(worker["environment"]))
		if environmentFilter != "" && !shared.EnvironmentsShareNamespace(workerEnvironment, environmentFilter) {
			continue
		}

		workerID := shared.StringValue(worker["id"])
		workerName := shared.StringValue(worker["name"])
		cluster := shared.StringValue(worker["cluster"])
		for _, raw := range shared.ToInterfaceSlice(worker["discoveredWorkloads"]) {
			workload := shared.MapPayload(raw)
			name := strings.TrimSpace(shared.StringValue(workload["name"]))
			kind := strings.TrimSpace(shared.StringValue(workload["kind"]))
			namespace := strings.TrimSpace(shared.StringValue(workload["namespace"]))
			if name == "" || kind == "" || namespace == "" {
				continue
			}
			key := strings.Join([]string{cluster, namespace, kind, name}, "|")
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}

			templateKind := "service"
			if strings.EqualFold(kind, "CronJob") {
				templateKind = "scheduled-job"
			}

			item := bson.M{
				"id":              key,
				"workerId":        workerID,
				"workerName":      workerName,
				"environment":     workerEnvironment,
				"cluster":         cluster,
				"namespace":       namespace,
				"kind":            kind,
				"name":            name,
				"images":          shared.ToStringSlice(workload["images"]),
				"primaryImage":    shared.StringValue(workload["primaryImage"]),
				"ports":           toPositiveIntSlice(shared.ToInterfaceSlice(workload["ports"])),
				"port":            shared.IntValue(workload["port"]),
				"replicas":        shared.IntValue(workload["replicas"]),
				"scheduleCron":    shared.StringValue(workload["scheduleCron"]),
				"healthCheckPath": shared.StringValue(workload["healthCheckPath"]),
				"serviceType":     "microservice",
				"templateKind":    templateKind,
				"sourceType":      "registry",
			}
			workloads = append(workloads, item)
		}
	}

	sort.Slice(workloads, func(i, j int) bool {
		left := strings.Join([]string{
			shared.StringValue(workloads[i]["environment"]),
			shared.StringValue(workloads[i]["kind"]),
			shared.StringValue(workloads[i]["name"]),
		}, "|")
		right := strings.Join([]string{
			shared.StringValue(workloads[j]["environment"]),
			shared.StringValue(workloads[j]["kind"]),
			shared.StringValue(workloads[j]["name"]),
		}, "|")
		return left < right
	})

	c.JSON(http.StatusOK, workloads)
}

func workerHeartbeatFresh(worker bson.M, threshold time.Time) bool {
	lastHeartbeat := strings.TrimSpace(shared.StringValue(worker["lastHeartbeat"]))
	if lastHeartbeat == "" {
		return false
	}
	parsed, err := time.Parse(time.RFC3339, lastHeartbeat)
	if err != nil {
		return false
	}
	return !parsed.Before(threshold)
}

func toPositiveIntSlice(values []interface{}) []int {
	if len(values) == 0 {
		return nil
	}
	out := make([]int, 0, len(values))
	for _, raw := range values {
		value := shared.IntValue(raw)
		if value > 0 {
			out = append(out, value)
		}
	}
	return out
}
