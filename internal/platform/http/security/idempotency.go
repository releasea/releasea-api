package security

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	httpheaders "releaseaapi/internal/platform/http/headers"
	"releaseaapi/internal/platform/shared"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	defaultIdempotencyTTL      = 10 * time.Minute
	defaultIdempotencyMaxKeySz = 128
)

type idempotencyResponse struct {
	Status      int    `bson:"status"`
	ContentType string `bson:"contentType,omitempty"`
	Body        []byte `bson:"body,omitempty"`
}

type idempotencyEntry struct {
	Key       string              `bson:"_id,omitempty"`
	State     string              `bson:"state"`
	ExpiresAt time.Time           `bson:"expiresAt"`
	Response  idempotencyResponse `bson:"response,omitempty"`
	UpdatedAt time.Time           `bson:"updatedAt"`
}

type idempotencyBackend interface {
	start(context.Context, string, time.Time) (bool, error)
	complete(context.Context, string, time.Time, idempotencyResponse) error
	fail(context.Context, string) error
	lookup(context.Context, string, time.Time) (idempotencyEntry, bool, error)
}

type idempotencyStore struct {
	mu      sync.Mutex
	entries map[string]idempotencyEntry
}

var idempotencyState = &idempotencyStore{
	entries: map[string]idempotencyEntry{},
}

var mongoIdempotencyState mongoIdempotencyStore

func activeIdempotencyBackend() idempotencyBackend {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("IDEMPOTENCY_BACKEND")), "mongo") {
		return mongoIdempotencyState
	}
	return idempotencyState
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

		backend := activeIdempotencyBackend()
		shouldProceed, err := backend.start(c.Request.Context(), fullKey, now)
		if err != nil {
			shared.RespondError(c, http.StatusServiceUnavailable, "Idempotency service unavailable")
			c.Abort()
			return
		}
		if !shouldProceed {
			if replayed, ok := replayIdempotentResponse(c, backend, fullKey, now); ok {
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
			_ = backend.fail(c.Request.Context(), fullKey)
			return
		}

		_ = backend.complete(c.Request.Context(), fullKey, now, idempotencyResponse{
			Status:      status,
			ContentType: recorder.header().Get(httpheaders.HeaderContentType),
			Body:        recorder.body.Bytes(),
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
		if now.After(entry.ExpiresAt) {
			delete(s.entries, key)
		}
	}
}

func (s *idempotencyStore) start(_ context.Context, key string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(now)

	if entry, exists := s.entries[key]; exists {
		if entry.State == "completed" {
			return false, nil
		}
		if entry.State == "processing" {
			return false, nil
		}
	}

	s.entries[key] = idempotencyEntry{
		Key:       key,
		State:     "processing",
		ExpiresAt: now.Add(defaultIdempotencyTTL),
		UpdatedAt: now,
	}
	return true, nil
}

func (s *idempotencyStore) complete(_ context.Context, key string, now time.Time, response idempotencyResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = idempotencyEntry{
		Key:       key,
		State:     "completed",
		ExpiresAt: now.Add(defaultIdempotencyTTL),
		UpdatedAt: now,
		Response:  response,
	}
	return nil
}

func (s *idempotencyStore) fail(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, exists := s.entries[key]
	if !exists {
		return nil
	}
	if entry.State == "processing" {
		delete(s.entries, key)
	}
	return nil
}

func (s *idempotencyStore) lookup(_ context.Context, key string, now time.Time) (idempotencyEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanup(now)

	entry, exists := s.entries[key]
	if !exists {
		return idempotencyEntry{}, false, nil
	}
	return entry, true, nil
}

type mongoIdempotencyStore struct{}

func (mongoIdempotencyStore) start(ctx context.Context, key string, now time.Time) (bool, error) {
	entry := idempotencyEntry{Key: key, State: "processing", ExpiresAt: now.Add(defaultIdempotencyTTL), UpdatedAt: now}
	_, err := shared.Collection(shared.IdempotencyKeysCollection).InsertOne(ctx, entry)
	if err == nil {
		return true, nil
	}
	if !mongo.IsDuplicateKeyError(err) {
		return false, err
	}
	// TTL deletion is asynchronous. Remove a logically expired record and retry once.
	result, deleteErr := shared.Collection(shared.IdempotencyKeysCollection).DeleteOne(ctx, bson.M{"_id": key, "expiresAt": bson.M{"$lte": now}})
	if deleteErr != nil {
		return false, deleteErr
	}
	if result.DeletedCount == 1 {
		_, err = shared.Collection(shared.IdempotencyKeysCollection).InsertOne(ctx, entry)
		if err == nil {
			return true, nil
		}
		if !mongo.IsDuplicateKeyError(err) {
			return false, err
		}
	}
	return false, nil
}

func (mongoIdempotencyStore) complete(ctx context.Context, key string, now time.Time, response idempotencyResponse) error {
	_, err := shared.Collection(shared.IdempotencyKeysCollection).UpdateByID(ctx, key, bson.M{"$set": bson.M{
		"state": "completed", "expiresAt": now.Add(defaultIdempotencyTTL), "updatedAt": now, "response": response,
	}})
	return err
}

func (mongoIdempotencyStore) fail(ctx context.Context, key string) error {
	_, err := shared.Collection(shared.IdempotencyKeysCollection).DeleteOne(ctx, bson.M{"_id": key, "state": "processing"})
	return err
}

func (mongoIdempotencyStore) lookup(ctx context.Context, key string, now time.Time) (idempotencyEntry, bool, error) {
	var entry idempotencyEntry
	err := shared.Collection(shared.IdempotencyKeysCollection).FindOne(ctx, bson.M{"_id": key, "expiresAt": bson.M{"$gt": now}}).Decode(&entry)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return idempotencyEntry{}, false, nil
	}
	return entry, err == nil, err
}

func replayIdempotentResponse(c *gin.Context, backend idempotencyBackend, key string, now time.Time) (bool, bool) {
	entry, exists, err := backend.lookup(c.Request.Context(), key, now)
	if err != nil || !exists || entry.State != "completed" {
		return false, err == nil
	}
	if entry.Response.ContentType != "" {
		c.Header(httpheaders.HeaderContentType, entry.Response.ContentType)
	}
	c.Header("X-Idempotency-Replayed", "true")
	c.Status(entry.Response.Status)
	if len(entry.Response.Body) > 0 {
		_, _ = c.Writer.Write(entry.Response.Body)
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
