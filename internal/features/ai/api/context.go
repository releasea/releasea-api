package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type evidence struct {
	ID       string      `json:"id"`
	Type     string      `json:"type"`
	Label    string      `json:"label"`
	Data     interface{} `json:"data"`
	Observed string      `json:"observedAt,omitempty"`
}

type serviceContext struct {
	ServiceID   string     `json:"serviceId"`
	Environment string     `json:"environment,omitempty"`
	Evidence    []evidence `json:"evidence"`
	Truncated   bool       `json:"truncated,omitempty"`
}

func buildServiceContext(ctx context.Context, serviceID, environment string) (serviceContext, error) {
	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		return serviceContext{}, fmt.Errorf("service not found")
	}
	result := serviceContext{ServiceID: serviceID, Environment: strings.TrimSpace(environment)}
	result.Evidence = append(result.Evidence, evidence{ID: "service-config", Type: "service", Label: "Sanitized service configuration", Data: whitelist(service,
		"id", "name", "type", "status", "runtimeStatus", "projectId", "sourceType", "repoUrl", "branch", "rootDir", "dockerImage", "dockerfilePath", "port", "healthCheckPath", "minReplicas", "maxReplicas", "replicas", "managementMode", "autoDeploy", "autoDeployEnvironment", "deploymentStrategy", "workerTags", "preferredWorkerCluster", "preferredWorkerRegion", "createdAt", "updatedAt")})

	deployFilter := bson.M{"serviceId": serviceID}
	if result.Environment != "" {
		deployFilter["environment"] = result.Environment
	}
	deploys, err := findLimited(ctx, shared.DeploysCollection, deployFilter, bson.D{{Key: "startedAt", Value: -1}}, 8)
	if err == nil {
		for index, deploy := range deploys {
			deployData := whitelist(deploy, "id", "status", "environment", "version", "commit", "branch", "startedAt", "finishedAt", "updatedAt", "strategyStatus", "error", "message")
			logs := stringSlice(deploy["logs"], 40)
			if len(logs) > 0 {
				deployData["recentLogs"] = logs
			}
			result.Evidence = append(result.Evidence, evidence{ID: fmt.Sprintf("deploy-%d", index+1), Type: "deploy", Label: "Recent deploy", Data: deployData, Observed: shared.StringValue(deploy["updatedAt"])})
		}
	}

	logFilter := bson.M{"serviceId": serviceID}
	if result.Environment != "" {
		logFilter["environment"] = result.Environment
	}
	logs, err := findLimited(ctx, shared.LogsCollection, logFilter, bson.D{{Key: "timestamp", Value: -1}}, 80)
	if err == nil && len(logs) > 0 {
		clean := make([]bson.M, 0, len(logs))
		for _, entry := range logs {
			clean = append(clean, whitelist(entry, "timestamp", "level", "message", "environment", "pod", "container", "source"))
		}
		result.Evidence = append(result.Evidence, evidence{ID: "runtime-logs", Type: "logs", Label: "Recent runtime logs", Data: clean})
	}

	audits, err := findLimited(ctx, shared.PlatformAuditCollection, bson.M{"resourceId": serviceID}, bson.D{{Key: "createdAt", Value: -1}}, 20)
	if err == nil && len(audits) > 0 {
		clean := make([]bson.M, 0, len(audits))
		for _, entry := range audits {
			clean = append(clean, whitelist(entry, "action", "status", "message", "source", "createdAt"))
		}
		result.Evidence = append(result.Evidence, evidence{ID: "audit-events", Type: "audit", Label: "Recent platform events", Data: clean})
	}
	return result, nil
}

func findLimited(ctx context.Context, collection string, filter bson.M, sort bson.D, limit int64) ([]bson.M, error) {
	cursor, err := shared.Collection(collection).Find(ctx, filter, options.Find().SetSort(sort).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var items []bson.M
	return items, cursor.All(ctx, &items)
}

func whitelist(document bson.M, keys ...string) bson.M {
	result := bson.M{}
	for _, key := range keys {
		if value, ok := document[key]; ok {
			result[key] = sanitizeValue(value)
		}
	}
	return result
}

func sanitizeValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case string:
		return redactSensitive(typed)
	case bson.M:
		out := bson.M{}
		for key, item := range typed {
			if isSensitiveKey(key) {
				continue
			}
			out[key] = sanitizeValue(item)
		}
		return out
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, item := range typed {
			if isSensitiveKey(key) {
				continue
			}
			out[key] = sanitizeValue(item)
		}
		return out
	case bson.A:
		out := make(bson.A, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeValue(item))
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			out = append(out, sanitizeValue(item))
		}
		return out
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "_", ""), "-", ""))
	for _, marker := range []string{"password", "secret", "token", "apikey", "privatekey", "credential", "environment"} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func stringSlice(value interface{}, limit int) []string {
	items := []string{}
	switch typed := value.(type) {
	case bson.A:
		for _, item := range typed {
			if text := strings.TrimSpace(shared.StringValue(item)); text != "" {
				items = append(items, redactSensitive(text))
			}
		}
	case []string:
		for _, item := range typed {
			if text := strings.TrimSpace(item); text != "" {
				items = append(items, redactSensitive(text))
			}
		}
	case []interface{}:
		for _, item := range typed {
			if text := strings.TrimSpace(shared.StringValue(item)); text != "" {
				items = append(items, redactSensitive(text))
			}
		}
	}
	if len(items) > limit {
		return items[len(items)-limit:]
	}
	return items
}

func marshalContext(value serviceContext) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}

// limitServiceContext keeps the evidence bundle valid JSON while fitting the
// provider input budget. Oversized string values are compacted and evidence
// that still cannot fit is omitted from the exact bundle sent to the model.
func limitServiceContext(value serviceContext, maxBytes int) serviceContext {
	if maxBytes < 256 {
		maxBytes = 256
	}
	if encoded, err := json.Marshal(value); err == nil && len(encoded) <= maxBytes {
		return value
	}

	limited := serviceContext{
		ServiceID:   value.ServiceID,
		Environment: value.Environment,
		Evidence:    []evidence{},
		Truncated:   true,
	}
	for _, item := range value.Evidence {
		for _, stringLimit := range []int{4096, 2048, 1024, 512, 256, 128, 64} {
			candidateItem := item
			candidateItem.Data = truncateStrings(item.Data, stringLimit)
			candidate := limited
			candidate.Evidence = append(append([]evidence{}, limited.Evidence...), candidateItem)
			encoded, err := json.Marshal(candidate)
			if err == nil && len(encoded) <= maxBytes {
				limited = candidate
				break
			}
		}
	}
	return limited
}

func truncateStrings(value interface{}, limit int) interface{} {
	switch typed := value.(type) {
	case string:
		runes := []rune(typed)
		if len(runes) <= limit {
			return typed
		}
		return string(runes[:limit]) + "…"
	case bson.M:
		out := bson.M{}
		for key, item := range typed {
			out[key] = truncateStrings(item, limit)
		}
		return out
	case map[string]interface{}:
		out := map[string]interface{}{}
		for key, item := range typed {
			out[key] = truncateStrings(item, limit)
		}
		return out
	case bson.A:
		out := make(bson.A, 0, len(typed))
		for _, item := range typed {
			out = append(out, truncateStrings(item, limit))
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		for _, item := range typed {
			out = append(out, truncateStrings(item, limit))
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, truncateStrings(item, limit).(string))
		}
		return out
	default:
		return value
	}
}
