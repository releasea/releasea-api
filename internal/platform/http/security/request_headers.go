package security

import (
	"net/http"
	"net/url"
	"os"
	"strings"

	httpheaders "releaseaapi/internal/platform/http/headers"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

func RequiredBrowserHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		if !isBrowserRequest(c) {
			c.Next()
			return
		}

		correlationID := strings.TrimSpace(c.GetHeader(httpheaders.HeaderCorrelationID))
		if correlationID == "" {
			shared.RespondError(c, http.StatusBadRequest, "Missing required header X-Correlation-ID")
			c.Abort()
			return
		}
		c.Header(httpheaders.HeaderCorrelationID, correlationID)

		if isMutatingMethod(c.Request.Method) {
			requestedWith := strings.TrimSpace(c.GetHeader(httpheaders.HeaderRequestedWith))
			if !strings.EqualFold(requestedWith, "XMLHttpRequest") {
				shared.RespondError(c, http.StatusBadRequest, "Missing required header X-Requested-With")
				c.Abort()
				return
			}
		}
		c.Next()
	}
}

func ValidateBrowserOrigin(c *gin.Context) bool {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin == "" {
		return true
	}
	if origin == "null" {
		return false
	}

	parsedOrigin, err := url.Parse(origin)
	if err != nil {
		return false
	}
	originHost := strings.ToLower(parsedOrigin.Hostname())
	if originHost == "localhost" || originHost == "127.0.0.1" || originHost == "::1" {
		return true
	}

	rawOrigins := strings.TrimSpace(os.Getenv("CORS_ORIGINS"))
	if rawOrigins == "" {
		return false
	}
	if rawOrigins == "*" {
		return true
	}
	for _, allowed := range strings.Split(rawOrigins, ",") {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}
		if strings.EqualFold(allowed, origin) {
			return true
		}
	}
	return false
}

func isBrowserRequest(c *gin.Context) bool {
	return strings.TrimSpace(c.GetHeader("Origin")) != ""
}

func isMutatingMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}
