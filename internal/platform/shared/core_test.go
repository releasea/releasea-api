package shared

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRespondErrorWritesMessage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	RespondError(ctx, http.StatusBadRequest, "invalid payload")

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	if body["message"] != "invalid payload" {
		t.Fatalf("message = %q, want %q", body["message"], "invalid payload")
	}
}

func TestHealthReturnsOK(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	Health(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("status = %q, want %q", body["status"], "ok")
	}
}

func TestReadinessReflectsDatabaseState(t *testing.T) {
	previous := pingDatabase
	t.Cleanup(func() { pingDatabase = previous })

	for _, test := range []struct {
		name       string
		pingError  error
		wantStatus int
	}{
		{name: "ready", wantStatus: http.StatusOK},
		{name: "database unavailable", pingError: errors.New("offline"), wantStatus: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			pingDatabase = func(context.Context) error { return test.pingError }
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/health/ready", nil)
			Readiness(ctx)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, test.wantStatus)
			}
		})
	}
}

func TestNowISOReturnsRFC3339UTC(t *testing.T) {
	value := NowISO()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("NowISO should return RFC3339 value, got %q: %v", value, err)
	}
	if parsed.Location() != time.UTC {
		t.Fatalf("NowISO should be UTC, got %v", parsed.Location())
	}
}
