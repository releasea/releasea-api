package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"os"
	"strings"

	httpheaders "releaseaapi/internal/platform/http/headers"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

const (
	defaultCSRFCookieName = "releasea_csrf_token"
	defaultCSRFCookiePath = "/api/v1"
	defaultCSRFCookieTTL  = 12 * 60 * 60
)

func EnsureCSRFToken(c *gin.Context) string {
	token := ReadCSRFCookieToken(c)
	if token != "" {
		return token
	}
	return IssueCSRFToken(c)
}

func IssueCSRFToken(c *gin.Context) string {
	token, err := generateSecureToken()
	if err != nil {
		return ""
	}
	cfg := resolveCSRFCookieConfig(c)
	c.SetSameSite(cfg.sameSite)
	c.SetCookie(cfg.name, token, cfg.maxAgeSeconds, cfg.path, cfg.domain, cfg.secure, false)
	return token
}

func ReadCSRFCookieToken(c *gin.Context) string {
	token, err := c.Cookie(csrfCookieName())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(token)
}

func ClearCSRFCookie(c *gin.Context) {
	cfg := resolveCSRFCookieConfig(c)
	c.SetSameSite(cfg.sameSite)
	c.SetCookie(cfg.name, "", -1, cfg.path, cfg.domain, cfg.secure, false)
}

func ValidateCSRFToken(c *gin.Context) bool {
	headerToken := strings.TrimSpace(c.GetHeader(httpheaders.HeaderCSRFToken))
	cookieToken := ReadCSRFCookieToken(c)
	if headerToken == "" || cookieToken == "" {
		return false
	}
	if len(headerToken) != len(cookieToken) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(headerToken), []byte(cookieToken)) == 1
}

type csrfCookieConfig struct {
	name          string
	path          string
	domain        string
	secure        bool
	sameSite      http.SameSite
	maxAgeSeconds int
}

func resolveCSRFCookieConfig(c *gin.Context) csrfCookieConfig {
	sameSite := parseCSRFSameSite(shared.EnvOrDefault("AUTH_CSRF_COOKIE_SAMESITE", "lax"))
	secure := shared.EnvBool("AUTH_CSRF_COOKIE_SECURE", requestIsSecure(c))
	if sameSite == http.SameSiteNoneMode && !secure {
		secure = true
	}

	return csrfCookieConfig{
		name:          csrfCookieName(),
		path:          shared.EnvOrDefault("AUTH_CSRF_COOKIE_PATH", defaultCSRFCookiePath),
		domain:        strings.TrimSpace(os.Getenv("AUTH_CSRF_COOKIE_DOMAIN")),
		secure:        secure,
		sameSite:      sameSite,
		maxAgeSeconds: csrfCookieMaxAgeSeconds(),
	}
}

func csrfCookieName() string {
	return shared.EnvOrDefault("AUTH_CSRF_COOKIE_NAME", defaultCSRFCookieName)
}

func parseCSRFSameSite(raw string) http.SameSite {
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

func csrfCookieMaxAgeSeconds() int {
	if parsed := parsePositiveInt("AUTH_CSRF_COOKIE_MAX_AGE_SECONDS", 0); parsed > 0 {
		return parsed
	}
	return defaultCSRFCookieTTL
}

func generateSecureToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
