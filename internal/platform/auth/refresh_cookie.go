package auth

import (
	"net/http"
	"os"
	"strings"

	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

const (
	defaultRefreshCookieName = "releasea_refresh_token"
	defaultRefreshCookiePath = "/api/v1/auth"
	defaultRefreshCookieTTL  = 168 * 60 * 60
)

func ReadRefreshCookieToken(c *gin.Context) string {
	token, err := c.Cookie(refreshCookieName())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

func SetRefreshCookie(c *gin.Context, refreshToken string) {
	token := strings.TrimSpace(refreshToken)
	if token == "" {
		return
	}
	cfg := resolveRefreshCookieConfig(c)
	c.SetSameSite(cfg.sameSite)
	c.SetCookie(cfg.name, token, cfg.maxAgeSeconds, cfg.path, cfg.domain, cfg.secure, true)
}

func ClearRefreshCookie(c *gin.Context) {
	cfg := resolveRefreshCookieConfig(c)
	c.SetSameSite(cfg.sameSite)
	c.SetCookie(cfg.name, "", -1, cfg.path, cfg.domain, cfg.secure, true)
}

type refreshCookieConfig struct {
	name          string
	path          string
	domain        string
	secure        bool
	sameSite      http.SameSite
	maxAgeSeconds int
}

func resolveRefreshCookieConfig(c *gin.Context) refreshCookieConfig {
	sameSite := parseSameSite(shared.EnvOrDefault("AUTH_REFRESH_COOKIE_SAMESITE", "lax"))
	secure := shared.EnvBool("AUTH_REFRESH_COOKIE_SECURE", requestIsSecure(c))
	if sameSite == http.SameSiteNoneMode && !secure {
		secure = true
	}

	return refreshCookieConfig{
		name:          refreshCookieName(),
		path:          shared.EnvOrDefault("AUTH_REFRESH_COOKIE_PATH", defaultRefreshCookiePath),
		domain:        strings.TrimSpace(os.Getenv("AUTH_REFRESH_COOKIE_DOMAIN")),
		secure:        secure,
		sameSite:      sameSite,
		maxAgeSeconds: refreshCookieMaxAgeSeconds(),
	}
}

func refreshCookieName() string {
	return shared.EnvOrDefault("AUTH_REFRESH_COOKIE_NAME", defaultRefreshCookieName)
}

func requestIsSecure(c *gin.Context) bool {
	if c.Request != nil && c.Request.TLS != nil {
		return true
	}
	forwardedProto := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")))
	return forwardedProto == "https"
}

func parseSameSite(raw string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	case "lax":
		return http.SameSiteLaxMode
	default:
		return http.SameSiteLaxMode
	}
}

func refreshCookieMaxAgeSeconds() int {
	if parsed := parsePositiveInt("AUTH_REFRESH_COOKIE_MAX_AGE_SECONDS", 0); parsed > 0 {
		return parsed
	}
	if ttlHours := parsePositiveInt("JWT_REFRESH_TTL_HOURS", 0); ttlHours > 0 {
		return ttlHours * 60 * 60
	}
	return defaultRefreshCookieTTL
}
