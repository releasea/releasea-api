package auth

import (
	"net/http"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

// RequireRoles rejects authenticated users whose role is not explicitly
// allowed. Authentication must run before this middleware.
func RequireRoles(roles ...string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if normalized := strings.ToLower(strings.TrimSpace(role)); normalized != "" {
			allowed[normalized] = struct{}{}
		}
	}
	return func(c *gin.Context) {
		role := strings.ToLower(strings.TrimSpace(c.GetString("authRole")))
		if _, ok := allowed[role]; !ok {
			shared.RespondError(c, http.StatusForbidden, "Insufficient permissions")
			c.Abort()
			return
		}
		c.Next()
	}
}
