package shared

import (
	"sort"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

func NormalizeWorkerPoolTags(tags []string) []string {
	normalized := NormalizeWorkerTags(tags)
	sort.Strings(normalized)
	return normalized
}

func WorkerPoolID(environment string, cluster string, namespacePrefix string, tags []string) string {
	tagKey := "-"
	normalizedTags := NormalizeWorkerPoolTags(tags)
	if len(normalizedTags) > 0 {
		tagKey = strings.Join(normalizedTags, ",")
	}
	return strings.Join([]string{
		strings.TrimSpace(environment),
		strings.TrimSpace(cluster),
		strings.TrimSpace(namespacePrefix),
		tagKey,
	}, "|")
}

func WorkerPoolIDFromWorker(worker bson.M) string {
	return WorkerPoolID(
		StringValue(worker["environment"]),
		StringValue(worker["cluster"]),
		StringValue(worker["namespacePrefix"]),
		ToStringSlice(worker["tags"]),
	)
}
