package services

import (
	"context"
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

type workerRoutingResolution struct {
	WorkerTags             []string
	PreferredWorkerCluster string
	PreferredWorkerRegion  string
}

func normalizeServiceWorkerRoutingPreferencesPayload(payload bson.M) {
	cluster := strings.TrimSpace(shared.StringValue(payload["preferredWorkerCluster"]))
	if cluster == "" {
		delete(payload, "preferredWorkerCluster")
	} else {
		payload["preferredWorkerCluster"] = cluster
	}

	region := strings.TrimSpace(shared.StringValue(payload["preferredWorkerRegion"]))
	if region == "" {
		delete(payload, "preferredWorkerRegion")
	} else {
		payload["preferredWorkerRegion"] = region
	}
}

func resolveServiceWorkerRouting(ctx context.Context, environment string, service bson.M) (workerRoutingResolution, error) {
	baseTags := serviceWorkerTags(service)
	preferredCluster := strings.TrimSpace(shared.StringValue(service["preferredWorkerCluster"]))
	preferredRegion := strings.TrimSpace(shared.StringValue(service["preferredWorkerRegion"]))
	regionTags := baseTags
	if preferredRegion != "" {
		regionTags = shared.NormalizeWorkerTags(append(regionTags, "region:"+preferredRegion))
	}

	if preferredCluster != "" && preferredRegion != "" {
		active, err := shared.HasActiveWorkerForEnvironmentTagsAndCluster(ctx, environment, regionTags, preferredCluster)
		if err != nil {
			return workerRoutingResolution{}, err
		}
		if active {
			return workerRoutingResolution{
				WorkerTags:             regionTags,
				PreferredWorkerCluster: preferredCluster,
				PreferredWorkerRegion:  preferredRegion,
			}, nil
		}
	}
	if preferredCluster != "" {
		active, err := shared.HasActiveWorkerForEnvironmentTagsAndCluster(ctx, environment, baseTags, preferredCluster)
		if err != nil {
			return workerRoutingResolution{}, err
		}
		if active {
			return workerRoutingResolution{
				WorkerTags:             baseTags,
				PreferredWorkerCluster: preferredCluster,
			}, nil
		}
	}
	if preferredRegion != "" {
		active, err := shared.HasActiveWorkerForEnvironmentAndTags(ctx, environment, regionTags)
		if err != nil {
			return workerRoutingResolution{}, err
		}
		if active {
			return workerRoutingResolution{
				WorkerTags:            regionTags,
				PreferredWorkerRegion: preferredRegion,
			}, nil
		}
	}

	return workerRoutingResolution{
		WorkerTags: baseTags,
	}, nil
}

func applyWorkerRoutingToPayload(payload bson.M, routing workerRoutingResolution) {
	normalizedWorkerTags := shared.NormalizeWorkerTags(routing.WorkerTags)
	if len(normalizedWorkerTags) > 0 {
		payload["workerTags"] = normalizedWorkerTags
	} else {
		delete(payload, "workerTags")
	}
	if strings.TrimSpace(routing.PreferredWorkerCluster) != "" {
		payload["preferredWorkerCluster"] = strings.TrimSpace(routing.PreferredWorkerCluster)
	} else {
		delete(payload, "preferredWorkerCluster")
	}
	if strings.TrimSpace(routing.PreferredWorkerRegion) != "" {
		payload["preferredWorkerRegion"] = strings.TrimSpace(routing.PreferredWorkerRegion)
	} else {
		delete(payload, "preferredWorkerRegion")
	}
}
