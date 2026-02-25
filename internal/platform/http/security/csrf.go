package security

import (
	"net/http"
	"strings"

	platformauth "releaseaapi/internal/platform/auth"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

func CSRFMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || !isMutatingMethod(c.Request.Method) {
			c.Next()
			return
		}
		if !isBrowserRequest(c) {
			c.Next()
			return
		}
		if !ValidateBrowserOrigin(c) {
			shared.RespondError(c, http.StatusForbidden, "Invalid request origin")
			c.Abort()
			return
		}
		if !platformauth.ValidateCSRFToken(c) {
			shared.RespondError(c, http.StatusForbidden, "Invalid CSRF token")
			c.Abort()
			return
		}
		c.Next()
	}
}

func CSRFMiddlewareForUserMutations() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || !isMutatingMethod(c.Request.Method) {
			c.Next()
			return
		}
		role := strings.ToLower(strings.TrimSpace(c.GetString("authRole")))
		if strings.HasPrefix(role, "worker") {
			c.Next()
			return
		}
		if !isBrowserRequest(c) {
			c.Next()
			return
		}
		if !ValidateBrowserOrigin(c) {
			shared.RespondError(c, http.StatusForbidden, "Invalid request origin")
			c.Abort()
			return
		}
		if !platformauth.ValidateCSRFToken(c) {
			shared.RespondError(c, http.StatusForbidden, "Invalid CSRF token")
			c.Abort()
			return
		}
		c.Next()
	}
}
