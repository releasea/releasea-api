package operations

import (
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

const (
	DeployStatusRequested   = "requested"
	DeployStatusScheduled   = "scheduled"
	DeployStatusPreparing   = "preparing"
	DeployStatusDeploying   = "deploying"
	DeployStatusValidating  = "validating"
	DeployStatusProgressing = "progressing"
	DeployStatusPromoting   = "promoting"
	DeployStatusCompleted   = "completed"
	DeployStatusRollingBack = "rolling-back"
	DeployStatusRolledBack  = "rolled-back"
	// DeployStatusRollback remains as a source-compatible alias for the
	// terminal outcome. New workers publish rolling-back while compensation is
	// running and rolled-back only after the previous version is restored.
	DeployStatusRollback = DeployStatusRolledBack
	DeployStatusFailed   = "failed"
	DeployStatusRetrying = "retrying"
)

var deployQueueBlockingStatuses = []string{
	DeployStatusRequested,
	DeployStatusScheduled,
	DeployStatusPreparing,
	DeployStatusDeploying,
	DeployStatusValidating,
	DeployStatusProgressing,
	DeployStatusPromoting,
	DeployStatusRetrying,
	DeployStatusRollingBack,
	StatusQueued,
	StatusInProgress,
}

var deploySuccessfulStatuses = []string{
	DeployStatusCompleted,
	"success",
}

var deployNonTerminalStatuses = []string{
	DeployStatusRequested,
	DeployStatusScheduled,
	DeployStatusPreparing,
	DeployStatusDeploying,
	DeployStatusValidating,
	DeployStatusProgressing,
	DeployStatusPromoting,
	DeployStatusRetrying,
	DeployStatusRollingBack,
	StatusQueued,
	StatusInProgress,
}

var deployKnownStatuses = map[string]struct{}{
	DeployStatusRequested:   {},
	DeployStatusScheduled:   {},
	DeployStatusPreparing:   {},
	DeployStatusDeploying:   {},
	DeployStatusValidating:  {},
	DeployStatusProgressing: {},
	DeployStatusPromoting:   {},
	DeployStatusCompleted:   {},
	DeployStatusRollingBack: {},
	DeployStatusRolledBack:  {},
	DeployStatusFailed:      {},
	DeployStatusRetrying:    {},
}

func DeployQueueBlockingStatuses() []string {
	return append([]string(nil), deployQueueBlockingStatuses...)
}

func DeploySuccessfulStatuses() []string {
	return append([]string(nil), deploySuccessfulStatuses...)
}

func DeployNonTerminalStatuses() []string {
	return append([]string(nil), deployNonTerminalStatuses...)
}

// DeployActiveKey identifies the single deploy allowed to be active for a
// service/environment pair. The field is removed when the deploy reaches a
// terminal state, allowing the next request to acquire the same key.
func DeployActiveKey(serviceID, environment string) string {
	return strings.TrimSpace(serviceID) + "|" + strings.ToLower(strings.TrimSpace(environment))
}

func NormalizeDeployStatus(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "success":
		return DeployStatusCompleted
	case "rollback":
		return DeployStatusRolledBack
	case StatusQueued:
		return DeployStatusScheduled
	case StatusInProgress:
		return DeployStatusDeploying
	default:
		return normalized
	}
}

func IsKnownDeployStatus(status string) bool {
	_, ok := deployKnownStatuses[NormalizeDeployStatus(status)]
	return ok
}

func NormalizeDeployDocument(doc bson.M) {
	if doc == nil {
		return
	}

	normalizedStatus := NormalizeDeployStatus(stringFromAny(doc["status"]))
	if normalizedStatus != "" {
		doc["status"] = normalizedStatus
	}

	if normalizedStrategy := normalizeDeployStrategyStatus(doc["strategyStatus"], normalizedStatus); normalizedStrategy != nil {
		doc["strategyStatus"] = normalizedStrategy
	}
}

func NormalizeDeployDocuments(docs []bson.M) {
	for _, doc := range docs {
		NormalizeDeployDocument(doc)
	}
}

func normalizeDeployStrategyStatus(raw interface{}, fallbackPhase string) bson.M {
	strategyStatus := toBSONMap(raw)
	if strategyStatus == nil {
		return nil
	}
	phase := NormalizeDeployStatus(stringFromAny(strategyStatus["phase"]))
	if phase == "" {
		phase = fallbackPhase
	}
	if phase != "" {
		strategyStatus["phase"] = phase
	}
	return strategyStatus
}

func toBSONMap(raw interface{}) bson.M {
	switch value := raw.(type) {
	case bson.M:
		return value
	case map[string]interface{}:
		return bson.M(value)
	default:
		return nil
	}
}

func stringFromAny(raw interface{}) string {
	if value, ok := raw.(string); ok {
		return strings.TrimSpace(value)
	}
	return ""
}

func CanTransitionDeployStatus(current, next string) bool {
	from := NormalizeDeployStatus(current)
	to := NormalizeDeployStatus(next)
	if from == "" || to == "" {
		return false
	}
	if from == to {
		return true
	}

	if from == DeployStatusCompleted || from == DeployStatusRolledBack || from == DeployStatusFailed {
		return false
	}
	if from == DeployStatusRollingBack {
		return to == DeployStatusRolledBack || to == DeployStatusFailed
	}
	if to == DeployStatusFailed || to == DeployStatusRollingBack {
		return true
	}
	if to == DeployStatusRetrying {
		return from != DeployStatusRequested && from != DeployStatusRetrying
	}

	// Status delivery is monotonic but may skip phases after a transient HTTP
	// failure. Accepting forward progress keeps a delayed earlier message from
	// regressing state while allowing the next observed phase to recover it.
	toRank, toKnown := deployProgressRank[to]
	if !toKnown {
		return false
	}
	if from == DeployStatusRetrying {
		return toRank >= deployProgressRank[DeployStatusPreparing]
	}
	fromRank, fromKnown := deployProgressRank[from]
	return fromKnown && toRank > fromRank
}

var deployProgressRank = map[string]int{
	DeployStatusRequested:   0,
	DeployStatusScheduled:   1,
	DeployStatusPreparing:   2,
	DeployStatusDeploying:   3,
	DeployStatusValidating:  4,
	DeployStatusProgressing: 5,
	DeployStatusPromoting:   6,
	DeployStatusCompleted:   7,
}
