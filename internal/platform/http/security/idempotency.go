package security

import (
	"bytes"
	"net/http"
	"strings"
	"sync"
	"time"

	httpheaders "releaseaapi/internal/platform/http/headers"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
)

const (
	defaultIdempotencyTTL      = 10 * time.Minute
	defaultIdempotencyMaxKeySz = 128
)

type idempotencyResponse struct {
	status      int
	contentType string
	body        []byte
}

type idempotencyEntry struct {
	state     string
	expiresAt time.Time
	response  idempotencyResponse
	updatedAt time.Time
}

type idempotencyStore struct {
	mu      sync.Mutex
	entries map[string]idempotencyEntry
}

var idempotencyState = &idempotencyStore{
	entries: map[string]idempotencyEntry{},
}

func RequireIdempotencyKey() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || !isMutatingMethod(c.Request.Method) {
			c.Next()
			return
		}

		key := strings.TrimSpace(c.GetHeader(httpheaders.HeaderIdempotency))
		if key == "" {
			shared.RespondError(c, http.StatusBadRequest, "Missing required header Idempotency-Key")
			c.Abort()
			return
		}
		if len(key) > defaultIdempotencyMaxKeySz {
			shared.RespondError(c, http.StatusBadRequest, "Invalid Idempotency-Key")
			c.Abort()
			return
		}

		fullKey := composeIdempotencyStoreKey(c, key)
		now := time.Now().UTC()

		shouldProceed := idempotencyState.start(fullKey, now)
		if !shouldProceed {
			if replayed, ok := idempotencyState.replay(c, fullKey, now); ok {
				if replayed {
					return
				}
			}
			shared.RespondError(c, http.StatusConflict, "Request with this Idempotency-Key is already being processed")
			c.Abort()
			return
		}

		recorder := &responseRecorder{
			ResponseWriter: c.Writer,
			body:           bytes.Buffer{},
		}
		c.Writer = recorder
		c.Next()

		status := recorder.status()
		if status >= 500 {
			idempotencyState.fail(fullKey, now)
			return
		}

		idempotencyState.complete(fullKey, now, idempotencyResponse{
			status:      status,
			contentType: recorder.header().Get(httpheaders.HeaderContentType),
			body:        recorder.body.Bytes(),
		})
	}
}

func composeIdempotencyStoreKey(c *gin.Context, provided string) string {
	userID := strings.TrimSpace(c.GetString("authUserId"))
	if userID == "" {
		userID = strings.TrimSpace(c.GetString("authRole"))
	}
	path := strings.TrimSpace(c.FullPath())
	if path == "" {
		path = strings.TrimSpace(c.Request.URL.Path)
	}
	return strings.Join([]string{
		strings.ToUpper(strings.TrimSpace(c.Request.Method)),
		path,
		userID,
		provided,
	}, "|")
}

func (s *idempotencyStore) cleanup(now time.Time) {
	for key, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, key)
		}
	}
}

func (s *idempotencyStore) start(key string, now time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(now)

	if entry, exists := s.entries[key]; exists {
		if entry.state == "completed" {
			return false
		}
		if entry.state == "processing" {
			return false
		}
	}

	s.entries[key] = idempotencyEntry{
		state:     "processing",
		expiresAt: now.Add(defaultIdempotencyTTL),
		updatedAt: now,
	}
	return true
}

func (s *idempotencyStore) complete(key string, now time.Time, response idempotencyResponse) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = idempotencyEntry{
		state:     "completed",
		expiresAt: now.Add(defaultIdempotencyTTL),
		updatedAt: now,
		response:  response,
	}
}

func (s *idempotencyStore) fail(key string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.entries[key]
	if !exists {
		return
	}
	if entry.state == "processing" {
		delete(s.entries, key)
	}
}

func (s *idempotencyStore) replay(c *gin.Context, key string, now time.Time) (bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(now)

	entry, exists := s.entries[key]
	if !exists || entry.state != "completed" {
		return false, false
	}
	if entry.response.contentType != "" {
		c.Header(httpheaders.HeaderContentType, entry.response.contentType)
	}
	c.Header("X-Idempotency-Replayed", "true")
	c.Status(entry.response.status)
	if len(entry.response.body) > 0 {
		_, _ = c.Writer.Write(entry.response.body)
	}
	c.Abort()
	return true, true
}

type responseRecorder struct {
	gin.ResponseWriter
	body       bytes.Buffer
	statusCode int
}

func (rw *responseRecorder) Write(data []byte) (int, error) {
	if rw.statusCode == 0 {
		rw.statusCode = http.StatusOK
	}
	if len(data) > 0 {
		_, _ = rw.body.Write(data)
	}
	return rw.ResponseWriter.Write(data)
}

func (rw *responseRecorder) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseRecorder) status() int {
	if rw.statusCode == 0 {
		return http.StatusOK
	}
	return rw.statusCode
}

func (rw *responseRecorder) header() http.Header {
	return rw.ResponseWriter.Header()
}
