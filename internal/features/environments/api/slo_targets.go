package environments

import (
	"fmt"
	"math"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	defaultEnvironmentAvailabilityTargetPct = 99.5
	defaultEnvironmentLatencyTargetMs       = 500
)

type sloTargetsPayload struct {
	AvailabilityPct *float64 `json:"availabilityPct"`
	LatencyP95Ms    *int     `json:"latencyP95Ms"`
}

func defaultEnvironmentSLOTargets() bson.M {
	return bson.M{
		"availabilityPct": defaultEnvironmentAvailabilityTargetPct,
		"latencyP95Ms":    defaultEnvironmentLatencyTargetMs,
	}
}

func normalizeEnvironmentSLOTargets(payload *sloTargetsPayload) (bson.M, error) {
	targets := defaultEnvironmentSLOTargets()
	if payload == nil {
		return targets, nil
	}

	if payload.AvailabilityPct != nil {
		value := math.Round(*payload.AvailabilityPct*1000) / 1000
		if value <= 0 || value > 100 {
			return nil, fmt.Errorf("availability target must be greater than 0 and at most 100")
		}
		targets["availabilityPct"] = value
	}

	if payload.LatencyP95Ms != nil {
		if *payload.LatencyP95Ms <= 0 {
			return nil, fmt.Errorf("latency target must be greater than 0")
		}
		targets["latencyP95Ms"] = *payload.LatencyP95Ms
	}

	return targets, nil
}
