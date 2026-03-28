package services

import (
	"strings"

	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func normalizeServiceWorkerTags(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		parts := strings.Split(typed, ",")
		tags := make([]string, 0, len(parts))
		for _, part := range parts {
			tags = append(tags, strings.TrimSpace(part))
		}
		return shared.NormalizeWorkerTags(tags)
	case []string:
		return shared.NormalizeWorkerTags(typed)
	case []interface{}:
		return shared.NormalizeWorkerTags(shared.ToStringSlice(typed))
	default:
		return shared.NormalizeWorkerTags(shared.ToStringSlice(value))
	}
}

func normalizeServiceWorkerTagsPayload(payload bson.M) {
	value, ok := payload["workerTags"]
	if !ok {
		return
	}
	tags := normalizeServiceWorkerTags(value)
	if len(tags) == 0 {
		delete(payload, "workerTags")
		return
	}
	payload["workerTags"] = tags
}

func serviceWorkerTags(service bson.M) []string {
	return normalizeServiceWorkerTags(service["workerTags"])
}
