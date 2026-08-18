package security

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireIdempotencyKeyReplaysCompletedResponse(t *testing.T) {
	t.Setenv("IDEMPOTENCY_BACKEND", "memory")
	gin.SetMode(gin.TestMode)
	idempotencyState = &idempotencyStore{entries: map[string]idempotencyEntry{}}

	var calls atomic.Int32
	router := gin.New()
	router.POST("/deploys", func(c *gin.Context) {
		c.Set("authUserId", "user-1")
		c.Next()
	}, RequireIdempotencyKey(), func(c *gin.Context) {
		calls.Add(1)
		c.JSON(http.StatusCreated, gin.H{"id": "deploy-1"})
	})

	perform := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/deploys", nil)
		request.Header.Set("Idempotency-Key", "same-request")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}

	first := perform()
	second := perform()
	if first.Code != http.StatusCreated || second.Code != http.StatusCreated {
		t.Fatalf("expected both responses to be 201, got %d and %d", first.Code, second.Code)
	}
	if calls.Load() != 1 {
		t.Fatalf("expected handler to run once, ran %d times", calls.Load())
	}
	if second.Header().Get("X-Idempotency-Replayed") != "true" {
		t.Fatal("expected replay marker on the second response")
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("expected replayed body %q, got %q", first.Body.String(), second.Body.String())
	}
}

func TestRequireIdempotencyKeyRejectsMissingKey(t *testing.T) {
	t.Setenv("IDEMPOTENCY_BACKEND", "memory")
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/deploys", RequireIdempotencyKey(), func(c *gin.Context) {
		c.Status(http.StatusCreated)
	})

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/deploys", nil))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.Code)
	}
}
