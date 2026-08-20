package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequireRoles(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name       string
		role       string
		wantStatus int
	}{
		{name: "admin allowed", role: "admin", wantStatus: http.StatusNoContent},
		{name: "developer denied", role: "developer", wantStatus: http.StatusForbidden},
		{name: "missing role denied", role: "", wantStatus: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.PUT("/settings",
				func(c *gin.Context) { c.Set("authRole", test.role) },
				RequireRoles("admin"),
				func(c *gin.Context) { c.Status(http.StatusNoContent) },
			)
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPut, "/settings", nil)
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("expected %d, got %d", test.wantStatus, response.Code)
			}
		})
	}
}

func TestWorkerJWTAllowedForOperationalLogEndpoints(t *testing.T) {
	for _, path := range []string{
		"/api/v1/deploys/deploy-1/logs",
		"/api/v1/rules/rule-1/logs",
		"/api/v1/rule-deploys/rule-deploy-1/logs",
	} {
		if !workerJWTAllowed(http.MethodPost, path) {
			t.Fatalf("worker JWT should be allowed to append logs at %s", path)
		}
	}
}
