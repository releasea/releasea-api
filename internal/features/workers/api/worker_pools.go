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

var workerPoolsLoader = loadWorkerPools

func GetWorkerPools(c *gin.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), shared.DBTimeout)
	defer cancel()

	pools, err := workerPoolsLoader(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load worker pools")
		return
	}

	c.JSON(http.StatusOK, pools)
}

func loadWorkerPools(ctx context.Context) ([]bson.M, error) {
	items, err := shared.FindAll(ctx, shared.Collection(shared.WorkersCollection), bson.M{})
	if err != nil {
		return nil, err
	}

	staleSeconds := getWorkerStaleSeconds()
	now := time.Now()
	for _, item := range items {
		lastHeartbeat := shared.StringValue(item["lastHeartbeat"])
		if lastHeartbeat == "" {
			item["status"] = "offline"
			item["isStale"] = true
			markRegistrationInactive(ctx, item, now)
			continue
		}
		parsed, err := time.Parse(time.RFC3339, lastHeartbeat)
		if err != nil {
			item["status"] = "offline"
			item["isStale"] = true
			markRegistrationInactive(ctx, item, now)
			continue
		}
		if now.Sub(parsed) > time.Duration(staleSeconds)*time.Second {
			item["status"] = "offline"
			item["isStale"] = true
			markRegistrationInactive(ctx, item, now)
		} else {
			item["isStale"] = false
		}
	}

	registrations, err := shared.FindAll(ctx, shared.Collection(shared.WorkerRegistrationsCollection), bson.M{
		"status": bson.M{"$ne": "revoked"},
	})
	if err != nil {
		return nil, err
	}

	controls, err := shared.FindAll(ctx, shared.Collection(shared.WorkerPoolControlsCollection), bson.M{})
	if err != nil {
		return nil, err
	}

	return summarizeWorkerPools(summarizeWorkers(items), registrations, controls), nil
}

func summarizeWorkerPools(workers []bson.M, registrations []bson.M, controls []bson.M) []bson.M {
	type poolMeta struct {
		tags       map[string]struct{}
		namespaces map[string]struct{}
	}

	pools := make(map[string]bson.M)
	metadata := make(map[string]*poolMeta)
	heartbeats := make(map[string]time.Time)
	controlByPoolID := make(map[string]bson.M, len(controls))
	for _, control := range controls {
		poolID := strings.TrimSpace(shared.StringValue(control["poolId"]))
		if poolID == "" {
			continue
		}
		controlByPoolID[poolID] = control
	}

	ensurePool := func(environment string, cluster string, namespacePrefix string, tags []string) bson.M {
		key := shared.WorkerPoolID(environment, cluster, namespacePrefix, tags)
		if pool, ok := pools[key]; ok {
			return pool
		}

		pool := bson.M{
			"id":                    key,
			"status":                "offline",
			"environment":           environment,
			"cluster":               cluster,
			"namespacePrefix":       namespacePrefix,
			"tags":                  tags,
			"namespaces":            []string{},
			"workerCount":           0,
			"onlineWorkers":         0,
			"busyWorkers":           0,
			"offlineWorkers":        0,
			"pendingWorkers":        0,
			"registrationCount":     0,
			"activeRegistrations":   0,
			"pendingRegistrations":  0,
			"inactiveRegistrations": 0,
			"desiredAgents":         0,
			"onlineAgents":          0,
			"lastHeartbeat":         "",
		}
		pools[key] = pool
		metadata[key] = &poolMeta{
			tags:       make(map[string]struct{}),
			namespaces: make(map[string]struct{}),
		}
		for _, tag := range tags {
			metadata[key].tags[tag] = struct{}{}
		}
		return pool
	}

	addNamespace := func(key string, namespace string) {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			return
		}
		if metadata[key] == nil {
			metadata[key] = &poolMeta{
				tags:       make(map[string]struct{}),
				namespaces: make(map[string]struct{}),
			}
		}
		metadata[key].namespaces[namespace] = struct{}{}
	}

	addTags := func(key string, tags []string) {
		if metadata[key] == nil {
			metadata[key] = &poolMeta{
				tags:       make(map[string]struct{}),
				namespaces: make(map[string]struct{}),
			}
		}
		for _, tag := range tags {
			if tag == "" {
				continue
			}
			metadata[key].tags[tag] = struct{}{}
		}
	}

	for _, worker := range workers {
		environment := strings.TrimSpace(shared.StringValue(worker["environment"]))
		cluster := strings.TrimSpace(shared.StringValue(worker["cluster"]))
		namespacePrefix := strings.TrimSpace(shared.StringValue(worker["namespacePrefix"]))
		tags := shared.NormalizeWorkerPoolTags(shared.ToStringSlice(worker["tags"]))
		key := shared.WorkerPoolID(environment, cluster, namespacePrefix, tags)
		pool := ensurePool(environment, cluster, namespacePrefix, tags)

		pool["workerCount"] = shared.IntValue(pool["workerCount"]) + 1
		pool["desiredAgents"] = shared.IntValue(pool["desiredAgents"]) + shared.IntValue(worker["desiredAgents"])
		pool["onlineAgents"] = shared.IntValue(pool["onlineAgents"]) + shared.IntValue(worker["onlineAgents"])

		switch strings.ToLower(strings.TrimSpace(shared.StringValue(worker["status"]))) {
		case "busy":
			pool["busyWorkers"] = shared.IntValue(pool["busyWorkers"]) + 1
		case "online":
			pool["onlineWorkers"] = shared.IntValue(pool["onlineWorkers"]) + 1
		case "pending":
			pool["pendingWorkers"] = shared.IntValue(pool["pendingWorkers"]) + 1
		default:
			pool["offlineWorkers"] = shared.IntValue(pool["offlineWorkers"]) + 1
		}

		addNamespace(key, shared.StringValue(worker["namespace"]))
		addTags(key, shared.ToStringSlice(worker["tags"]))

		lastHeartbeat := strings.TrimSpace(shared.StringValue(worker["lastHeartbeat"]))
		if lastHeartbeat == "" {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, lastHeartbeat)
		if err != nil {
			continue
		}
		if parsed.After(heartbeats[key]) {
			heartbeats[key] = parsed
			pool["lastHeartbeat"] = lastHeartbeat
		}
	}

	for _, registration := range registrations {
		environment := strings.TrimSpace(shared.StringValue(registration["environment"]))
		cluster := strings.TrimSpace(shared.StringValue(registration["cluster"]))
		namespacePrefix := strings.TrimSpace(shared.StringValue(registration["namespacePrefix"]))
		tags := shared.NormalizeWorkerPoolTags(shared.ToStringSlice(registration["tags"]))
		key := shared.WorkerPoolID(environment, cluster, namespacePrefix, tags)
		pool := ensurePool(environment, cluster, namespacePrefix, tags)

		pool["registrationCount"] = shared.IntValue(pool["registrationCount"]) + 1
		switch strings.ToLower(strings.TrimSpace(shared.StringValue(registration["status"]))) {
		case "unused":
			pool["pendingRegistrations"] = shared.IntValue(pool["pendingRegistrations"]) + 1
		case "active":
			pool["activeRegistrations"] = shared.IntValue(pool["activeRegistrations"]) + 1
		default:
			pool["inactiveRegistrations"] = shared.IntValue(pool["inactiveRegistrations"]) + 1
		}

		addNamespace(key, shared.StringValue(registration["namespace"]))
		addTags(key, shared.ToStringSlice(registration["tags"]))
	}

	out := make([]bson.M, 0, len(pools))
	for key, pool := range pools {
		tags := make([]string, 0, len(metadata[key].tags))
		for tag := range metadata[key].tags {
			tags = append(tags, tag)
		}
		sort.Strings(tags)
		namespaces := make([]string, 0, len(metadata[key].namespaces))
		for namespace := range metadata[key].namespaces {
			namespaces = append(namespaces, namespace)
		}
		sort.Strings(namespaces)

		pool["tags"] = tags
		pool["namespaces"] = namespaces
		if control, ok := controlByPoolID[key]; ok {
			maintenanceReason := shared.StringValue(control["maintenanceReason"])
			if maintenanceReason == "" {
				maintenanceReason = shared.StringValue(control["reason"])
			}
			maintenanceUpdatedAt := shared.StringValue(control["maintenanceUpdatedAt"])
			if maintenanceUpdatedAt == "" {
				maintenanceUpdatedAt = shared.StringValue(control["updatedAt"])
			}
			maintenanceUpdatedBy := shared.StringValue(control["maintenanceUpdatedBy"])
			if maintenanceUpdatedBy == "" {
				maintenanceUpdatedBy = shared.StringValue(control["updatedBy"])
			}
			pool["maintenanceEnabled"] = shared.BoolValue(control["maintenanceEnabled"])
			pool["maintenanceReason"] = maintenanceReason
			pool["maintenanceUpdatedAt"] = maintenanceUpdatedAt
			pool["maintenanceUpdatedBy"] = maintenanceUpdatedBy
			pool["drainEnabled"] = shared.BoolValue(control["drainEnabled"])
			pool["drainReason"] = shared.StringValue(control["drainReason"])
			pool["drainUpdatedAt"] = shared.StringValue(control["drainUpdatedAt"])
			pool["drainUpdatedBy"] = shared.StringValue(control["drainUpdatedBy"])
		} else {
			pool["maintenanceEnabled"] = false
			pool["drainEnabled"] = false
		}
		pool["status"] = workerPoolStatus(pool)
		pool["availableAgents"] = workerPoolAvailableAgents(pool)
		pool["capacityScore"] = workerPoolCapacityScore(pool)
		pool["capacityState"] = workerPoolCapacityState(pool)
		pool["saturationPercent"] = workerPoolSaturationPercent(pool)
		pool["saturationState"] = workerPoolSaturationState(pool)
		out = append(out, pool)
	}

	sort.Slice(out, func(i, j int) bool {
		left := workerPoolSortKey(out[i])
		right := workerPoolSortKey(out[j])
		return left < right
	})

	return out
}

func workerPoolSortKey(pool bson.M) string {
	tags := strings.Join(shared.ToStringSlice(pool["tags"]), ",")
	return strings.Join([]string{
		strings.TrimSpace(shared.StringValue(pool["environment"])),
		strings.TrimSpace(shared.StringValue(pool["cluster"])),
		strings.TrimSpace(shared.StringValue(pool["namespacePrefix"])),
		tags,
	}, "|")
}

func workerPoolStatus(pool bson.M) string {
	if shared.IntValue(pool["busyWorkers"]) > 0 {
		return "busy"
	}
	if shared.IntValue(pool["onlineWorkers"]) > 0 {
		return "online"
	}
	if shared.IntValue(pool["pendingWorkers"]) > 0 || shared.IntValue(pool["pendingRegistrations"]) > 0 || shared.IntValue(pool["activeRegistrations"]) > 0 {
		return "pending"
	}
	return "offline"
}

func workerPoolAvailableAgents(pool bson.M) int {
	available := shared.IntValue(pool["onlineAgents"]) - shared.IntValue(pool["busyWorkers"])
	if available < 0 {
		return 0
	}
	return available
}

func workerPoolCapacityScore(pool bson.M) int {
	desiredAgents := shared.IntValue(pool["desiredAgents"])
	onlineAgents := shared.IntValue(pool["onlineAgents"])
	readyWorkers := shared.IntValue(pool["onlineWorkers"]) + shared.IntValue(pool["busyWorkers"])
	workerCount := shared.IntValue(pool["workerCount"])
	pendingRegistrations := shared.IntValue(pool["pendingRegistrations"])
	busyWorkers := shared.IntValue(pool["busyWorkers"])

	score := 0
	if desiredAgents > 0 {
		effectiveOnline := onlineAgents
		if effectiveOnline > desiredAgents {
			effectiveOnline = desiredAgents
		}
		score += effectiveOnline * 60 / desiredAgents
	} else if onlineAgents > 0 {
		score += 60
	}

	if workerCount > 0 {
		score += readyWorkers * 25 / workerCount
	}

	if pendingRegistrations > 0 {
		bonus := pendingRegistrations * 5
		if bonus > 15 {
			bonus = 15
		}
		score += bonus
	}

	if readyWorkers > 0 {
		score -= busyWorkers * 15 / readyWorkers
	}

	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func workerPoolCapacityState(pool bson.M) string {
	if shared.BoolValue(pool["maintenanceEnabled"]) {
		return "maintenance"
	}
	if shared.BoolValue(pool["drainEnabled"]) {
		return "draining"
	}
	status := workerPoolStatus(pool)
	switch {
	case status == "offline":
		return "unavailable"
	case shared.IntValue(pool["onlineAgents"]) == 0 && shared.IntValue(pool["pendingRegistrations"]) > 0:
		return "bootstrap"
	}

	score := workerPoolCapacityScore(pool)
	switch {
	case score >= 75:
		return "ready"
	case score >= 40:
		return "constrained"
	default:
		return "degraded"
	}
}

func workerPoolSaturationPercent(pool bson.M) int {
	if shared.BoolValue(pool["maintenanceEnabled"]) || shared.BoolValue(pool["drainEnabled"]) {
		return 0
	}
	base := shared.IntValue(pool["onlineAgents"])
	if base <= 0 {
		base = shared.IntValue(pool["onlineWorkers"]) + shared.IntValue(pool["busyWorkers"])
	}
	if base <= 0 {
		return 0
	}
	used := shared.IntValue(pool["busyWorkers"])
	if used <= 0 {
		return 0
	}
	if used >= base {
		return 100
	}
	return used * 100 / base
}

func workerPoolSaturationState(pool bson.M) string {
	if shared.BoolValue(pool["maintenanceEnabled"]) {
		return "maintenance"
	}
	if shared.BoolValue(pool["drainEnabled"]) {
		return "draining"
	}
	if workerPoolStatus(pool) == "offline" {
		return "unavailable"
	}
	percent := workerPoolSaturationPercent(pool)
	switch {
	case percent >= 90:
		return "saturated"
	case percent >= 70:
		return "hot"
	case percent >= 30:
		return "active"
	default:
		return "idle"
	}
}
