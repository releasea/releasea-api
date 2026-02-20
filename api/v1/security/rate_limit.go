package security

import (
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"releaseaapi/api/v1/shared"

	"github.com/gin-gonic/gin"
)

type rateLimitBucket struct {
	Count   int
	ResetAt time.Time
}

type authRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateLimitBucket
	limit   int
	window  time.Duration
}

var (
	authLimiterOnce sync.Once
	authLimiterInst *authRateLimiter
)

func AuthRateLimitMiddleware() gin.HandlerFunc {
	limiter := authLimiter()
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		now := time.Now().UTC()
		key := buildRateLimitKey(c)
		allowed, remaining, retryAfter := limiter.allow(key, now)
		c.Header("X-RateLimit-Limit", strconv.Itoa(limiter.limit))
		c.Header("X-RateLimit-Remaining", strconv.Itoa(remaining))
		if !allowed {
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			shared.RespondError(c, http.StatusTooManyRequests, "Too many requests. Please retry shortly.")
			c.Abort()
			return
		}
		c.Next()
	}
}

func authLimiter() *authRateLimiter {
	authLimiterOnce.Do(func() {
		limit := parsePositiveInt("AUTH_RATE_LIMIT_MAX_REQUESTS", 20)
		windowSeconds := parsePositiveInt("AUTH_RATE_LIMIT_WINDOW_SECONDS", 60)
		authLimiterInst = &authRateLimiter{
			buckets: make(map[string]*rateLimitBucket),
			limit:   limit,
			window:  time.Duration(windowSeconds) * time.Second,
		}
	})
	return authLimiterInst
}

func (l *authRateLimiter) allow(key string, now time.Time) (allowed bool, remaining int, retryAfter int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cleanup(now)

	bucket, ok := l.buckets[key]
	if !ok || now.After(bucket.ResetAt) {
		bucket = &rateLimitBucket{
			Count:   0,
			ResetAt: now.Add(l.window),
		}
		l.buckets[key] = bucket
	}

	if bucket.Count >= l.limit {
		wait := int(bucket.ResetAt.Sub(now).Seconds())
		if wait < 1 {
			wait = 1
		}
		return false, 0, wait
	}

	bucket.Count++
	remaining = l.limit - bucket.Count
	if remaining < 0 {
		remaining = 0
	}
	return true, remaining, 0
}

func (l *authRateLimiter) cleanup(now time.Time) {
	if len(l.buckets) < 1000 {
		return
	}
	for key, bucket := range l.buckets {
		if now.After(bucket.ResetAt) {
			delete(l.buckets, key)
		}
	}
}

func buildRateLimitKey(c *gin.Context) string {
	ip := strings.TrimSpace(c.ClientIP())
	if ip == "" {
		ip = "unknown"
	}
	route := strings.TrimSpace(c.FullPath())
	if route == "" {
		route = strings.TrimSpace(c.Request.URL.Path)
	}
	return ip + "|" + route
}

func parsePositiveInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
