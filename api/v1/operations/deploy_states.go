package operations

import "strings"

const (
	DeployStatusRequested   = "requested"
	DeployStatusScheduled   = "scheduled"
	DeployStatusPreparing   = "preparing"
	DeployStatusDeploying   = "deploying"
	DeployStatusValidating  = "validating"
	DeployStatusProgressing = "progressing"
	DeployStatusPromoting   = "promoting"
	DeployStatusCompleted   = "completed"
	DeployStatusRollback    = "rollback"
	DeployStatusFailed      = "failed"
	DeployStatusRetrying    = "retrying"
)

var deployQueueBlockingStatuses = []string{
	DeployStatusScheduled,
	DeployStatusPreparing,
	DeployStatusDeploying,
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
	DeployStatusRollback,
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
	DeployStatusRollback:    {},
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

func NormalizeDeployStatus(status string) string {
	normalized := strings.ToLower(strings.TrimSpace(status))
	switch normalized {
	case "success":
		return DeployStatusCompleted
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

func CanTransitionDeployStatus(current, next string) bool {
	from := NormalizeDeployStatus(current)
	to := NormalizeDeployStatus(next)
	if from == "" || to == "" {
		return false
	}
	if from == to {
		return true
	}

	switch from {
	case DeployStatusRequested:
		return to == DeployStatusScheduled || to == DeployStatusFailed
	case DeployStatusScheduled:
		return to == DeployStatusPreparing || to == DeployStatusRetrying || to == DeployStatusFailed || to == DeployStatusRollback
	case DeployStatusPreparing:
		return to == DeployStatusDeploying || to == DeployStatusRetrying || to == DeployStatusRollback || to == DeployStatusFailed
	case DeployStatusDeploying:
		return to == DeployStatusValidating || to == DeployStatusRetrying || to == DeployStatusRollback || to == DeployStatusFailed
	case DeployStatusValidating:
		return to == DeployStatusProgressing || to == DeployStatusPromoting || to == DeployStatusCompleted || to == DeployStatusRetrying || to == DeployStatusRollback || to == DeployStatusFailed
	case DeployStatusProgressing:
		return to == DeployStatusPromoting || to == DeployStatusRetrying || to == DeployStatusRollback || to == DeployStatusFailed
	case DeployStatusPromoting:
		return to == DeployStatusCompleted || to == DeployStatusRetrying || to == DeployStatusRollback || to == DeployStatusFailed
	case DeployStatusRetrying:
		return to == DeployStatusPreparing || to == DeployStatusDeploying || to == DeployStatusValidating || to == DeployStatusRollback || to == DeployStatusFailed
	case DeployStatusRollback:
		return to == DeployStatusFailed || to == DeployStatusCompleted
	case DeployStatusCompleted, DeployStatusFailed:
		return false
	default:
		return false
	}
}
