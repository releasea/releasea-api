package rules

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
)

func TestRespondIfObservedRuleManagementBlocked(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	blocked := respondIfObservedRuleManagementBlocked(ctx, bson.M{"managementMode": "observed"})
	if !blocked {
		t.Fatalf("expected observed service to be blocked")
	}
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestRespondIfObservedRuleManagementBlockedAllowsManagedService(t *testing.T) {
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	blocked := respondIfObservedRuleManagementBlocked(ctx, bson.M{"managementMode": "managed"})
	if blocked {
		t.Fatalf("managed service should not be blocked")
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}
