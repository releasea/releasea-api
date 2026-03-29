package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	platformmodels "releaseaapi/internal/platform/models"

	"github.com/gin-gonic/gin"
)

func TestGetControlPlaneMetricsReturnsJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	router := gin.New()
	router.GET("/api/v1/observability/control-plane", GetControlPlaneMetrics)

	previous := controlPlaneMetricsLoader
	controlPlaneMetricsLoader = func(context.Context) (platformmodels.ControlPlaneMetrics, error) {
		return platformmodels.ControlPlaneMetrics{
			Version: "1",
			Queue: platformmodels.ControlPlaneQueueMetrics{
				QueueName:                "releasea.worker",
				DeadLetterEnabled:        true,
				DeadLetterQueueName:      "releasea.worker.dead-letter",
				QueuedOperations:         2,
				DispatchingOperations:    1,
				DispatchFailedOperations: 0,
				StaleQueuedOperations:    0,
				RecentDispatchFailures:   0,
				Status:                   controlPlaneQueueStatusReview,
				Summary:                  "Operations are queued and waiting for worker execution.",
			},
		}, nil
	}
	defer func() {
		controlPlaneMetricsLoader = previous
	}()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/observability/control-plane", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var body platformmodels.ControlPlaneMetrics
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("response should be valid JSON: %v", err)
	}
	if body.Queue.QueueName != "releasea.worker" {
		t.Fatalf("queue name = %q, want %q", body.Queue.QueueName, "releasea.worker")
	}
	if body.Queue.Status != controlPlaneQueueStatusReview {
		t.Fatalf("queue status = %q, want %q", body.Queue.Status, controlPlaneQueueStatusReview)
	}
}
