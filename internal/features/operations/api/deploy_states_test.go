package operations

import (
	"testing"

	"go.mongodb.org/mongo-driver/bson"
)

func TestNormalizeDeployStatus(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{name: "maps success alias", input: "success", expect: DeployStatusCompleted},
		{name: "maps queued alias", input: StatusQueued, expect: DeployStatusScheduled},
		{name: "maps in-progress alias", input: StatusInProgress, expect: DeployStatusDeploying},
		{name: "normalizes whitespace and case", input: "  REQUESTED  ", expect: DeployStatusRequested},
		{name: "keeps unknown", input: "custom", expect: "custom"},
		{name: "empty remains empty", input: "", expect: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeDeployStatus(tt.input)
			if got != tt.expect {
				t.Fatalf("NormalizeDeployStatus(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestCanTransitionDeployStatusIncludesRetryAndRollbackPaths(t *testing.T) {
	tests := []struct {
		name    string
		current string
		next    string
		expect  bool
	}{
		{name: "scheduled to retrying", current: DeployStatusScheduled, next: DeployStatusRetrying, expect: true},
		{name: "retrying to preparing", current: DeployStatusRetrying, next: DeployStatusPreparing, expect: true},
		{name: "deploying to rollback", current: DeployStatusDeploying, next: DeployStatusRollback, expect: true},
		{name: "rollback to completed", current: DeployStatusRollback, next: DeployStatusCompleted, expect: true},
		{name: "rollback to failed", current: DeployStatusRollback, next: DeployStatusFailed, expect: true},
		{name: "queued alias to preparing", current: StatusQueued, next: DeployStatusPreparing, expect: true},
		{name: "completed to retrying denied", current: DeployStatusCompleted, next: DeployStatusRetrying, expect: false},
		{name: "failed to rollback denied", current: DeployStatusFailed, next: DeployStatusRollback, expect: false},
		{name: "requested to completed denied", current: DeployStatusRequested, next: DeployStatusCompleted, expect: false},
		{name: "invalid source denied", current: "unknown", next: DeployStatusPreparing, expect: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CanTransitionDeployStatus(tt.current, tt.next)
			if got != tt.expect {
				t.Fatalf("CanTransitionDeployStatus(%q, %q) = %v, want %v", tt.current, tt.next, got, tt.expect)
			}
		})
	}
}

func TestNormalizeDeployDocument(t *testing.T) {
	doc := bson.M{
		"status": StatusQueued,
		"strategyStatus": bson.M{
			"phase": StatusInProgress,
		},
	}

	NormalizeDeployDocument(doc)

	if got := doc["status"]; got != DeployStatusScheduled {
		t.Fatalf("status = %v, want %s", got, DeployStatusScheduled)
	}

	strategy := doc["strategyStatus"].(bson.M)
	if got := strategy["phase"]; got != DeployStatusDeploying {
		t.Fatalf("strategyStatus.phase = %v, want %s", got, DeployStatusDeploying)
	}
}

func TestNormalizeDeployDocumentUsesFallbackPhaseWhenMissing(t *testing.T) {
	doc := bson.M{
		"status": DeployStatusValidating,
		"strategyStatus": bson.M{
			"phase": "   ",
		},
	}

	NormalizeDeployDocument(doc)

	strategy := doc["strategyStatus"].(bson.M)
	if got := strategy["phase"]; got != DeployStatusValidating {
		t.Fatalf("strategyStatus.phase = %v, want %s", got, DeployStatusValidating)
	}
}

func TestNormalizeDeployDocuments(t *testing.T) {
	docs := []bson.M{
		{"status": StatusInProgress},
		{"status": "success"},
	}

	NormalizeDeployDocuments(docs)

	if got := docs[0]["status"]; got != DeployStatusDeploying {
		t.Fatalf("docs[0].status = %v, want %s", got, DeployStatusDeploying)
	}
	if got := docs[1]["status"]; got != DeployStatusCompleted {
		t.Fatalf("docs[1].status = %v, want %s", got, DeployStatusCompleted)
	}
}

func TestDeployStatusSlicesAreCopies(t *testing.T) {
	queueA := DeployQueueBlockingStatuses()
	queueA[0] = "mutated"
	queueB := DeployQueueBlockingStatuses()
	if queueB[0] == "mutated" {
		t.Fatalf("DeployQueueBlockingStatuses returned shared backing array")
	}

	successA := DeploySuccessfulStatuses()
	successA[0] = "mutated"
	successB := DeploySuccessfulStatuses()
	if successB[0] == "mutated" {
		t.Fatalf("DeploySuccessfulStatuses returned shared backing array")
	}

	nonTerminalA := DeployNonTerminalStatuses()
	nonTerminalA[0] = "mutated"
	nonTerminalB := DeployNonTerminalStatuses()
	if nonTerminalB[0] == "mutated" {
		t.Fatalf("DeployNonTerminalStatuses returned shared backing array")
	}
}

func TestIsKnownDeployStatus(t *testing.T) {
	if !IsKnownDeployStatus(StatusQueued) {
		t.Fatalf("expected queued alias to be recognized as known status")
	}
	if IsKnownDeployStatus("nonsense") {
		t.Fatalf("expected nonsense to be unknown status")
	}
}
