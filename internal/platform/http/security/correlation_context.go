package security

import (
	"strings"

	httpheaders "releaseaapi/internal/platform/http/headers"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

func CorrelationContextMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		correlationID := strings.TrimSpace(c.GetHeader(httpheaders.HeaderCorrelationID))
		if correlationID == "" {
			correlationID = shared.NewCorrelationID()
		}
		c.Request = c.Request.WithContext(shared.WithCorrelationID(c.Request.Context(), correlationID))
		c.Header(httpheaders.HeaderCorrelationID, correlationID)
		c.Next()
	}
}
