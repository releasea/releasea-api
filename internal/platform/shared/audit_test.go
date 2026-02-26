package shared

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAuditActorFromContextReturnsEmptyForNilContext(t *testing.T) {
	actorID, actorName, actorRole := AuditActorFromContext(nil)
	if actorID != "" || actorName != "" || actorRole != "" {
		t.Fatalf("expected empty actor values for nil context")
	}
}

func TestAuditActorFromContextUsesAuthFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("authUserId", " user-1 ")
	ctx.Set("authRole", " admin ")
	ctx.Set("authName", " Alice ")

	actorID, actorName, actorRole := AuditActorFromContext(ctx)
	if actorID != "user-1" {
		t.Fatalf("actorID = %q, want %q", actorID, "user-1")
	}
	if actorName != "Alice" {
		t.Fatalf("actorName = %q, want %q", actorName, "Alice")
	}
	if actorRole != "admin" {
		t.Fatalf("actorRole = %q, want %q", actorRole, "admin")
	}
}

func TestAuditActorFromContextFallsBackToEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set("authEmail", " user@example.com ")

	_, actorName, _ := AuditActorFromContext(ctx)
	if actorName != "user@example.com" {
		t.Fatalf("actorName = %q, want %q", actorName, "user@example.com")
	}
}

func TestRecordAuditEventSkipsWhenActionIsEmpty(t *testing.T) {
	RecordAuditEvent(context.Background(), AuditEvent{Action: "   "})
}
