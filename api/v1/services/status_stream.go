package services

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"releaseaapi/api/v1/operations"
	"releaseaapi/api/v1/shared"
	"releaseaapi/client"

	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	statusStreamHeartbeatInterval   = 20 * time.Second
	defaultStatusStreamPollInterval = 5 * time.Second
	minStatusStreamPollInterval     = time.Second
	maxStatusStreamPollInterval     = 60 * time.Second
)

type serviceStatusSnapshot struct {
	Service     bson.M   `json:"service"`
	Deploys     []bson.M `json:"deploys"`
	Rules       []bson.M `json:"rules"`
	RuleDeploys []bson.M `json:"ruleDeploys"`
	EmittedAt   string   `json:"emittedAt"`
}

type servicesStatusSnapshot struct {
	Services  []bson.M `json:"services"`
	Deploys   []bson.M `json:"deploys"`
	EmittedAt string   `json:"emittedAt"`
}

type serviceStatusChangeEvent struct {
	OperationType string `bson:"operationType"`
	Namespace     struct {
		Collection string `bson:"coll"`
	} `bson:"ns"`
	FullDocument bson.M `bson:"fullDocument"`
	DocumentKey  bson.M `bson:"documentKey"`
}

// snapshotEmitter loads a fresh snapshot and emits it if the digest changed.
// Returns (newDigest, keepConnectionOpen, error).
type snapshotEmitter func(c *gin.Context, currentDigest string) (string, bool, error)

// changeEventFilter decides whether a change stream event should trigger a snapshot reload.
type changeEventFilter func(event serviceStatusChangeEvent) bool

// --- Handlers -----------------------------------------------------------------

func GetServiceStatusSnapshot(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}

	environment := strings.TrimSpace(c.Query("environment"))
	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()

	snapshot, found, err := loadServiceStatusSnapshot(ctx, serviceID, environment)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service status")
		return
	}
	if !found {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	c.JSON(http.StatusOK, snapshot)
}

func StreamServiceStatus(c *gin.Context) {
	serviceID := strings.TrimSpace(c.Param("id"))
	if serviceID == "" {
		shared.RespondError(c, http.StatusBadRequest, "Service ID required")
		return
	}
	environment := strings.TrimSpace(c.Query("environment"))

	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	snapshot, found, err := loadServiceStatusSnapshot(ctx, serviceID, environment)
	cancel()
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load service status")
		return
	}
	if !found {
		shared.RespondError(c, http.StatusNotFound, "Service not found")
		return
	}

	emitter := serviceSnapshotEmitter(serviceID, environment)
	filter := func(event serviceStatusChangeEvent) bool {
		return isServiceStatusEventRelevant(event, serviceID)
	}
	collections := []string{
		shared.ServicesCollection,
		shared.DeploysCollection,
		shared.RulesCollection,
		shared.RuleDeploysCollection,
	}

	streamSSE(c, snapshot, collections, filter, emitter, "service:"+serviceID)
}

func GetServicesStatusSnapshot(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	defer cancel()

	snapshot, err := loadServicesStatusSnapshot(ctx)
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load services status")
		return
	}

	c.JSON(http.StatusOK, snapshot)
}

func StreamServicesStatus(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
	snapshot, err := loadServicesStatusSnapshot(ctx)
	cancel()
	if err != nil {
		shared.RespondError(c, http.StatusInternalServerError, "Failed to load services status")
		return
	}

	emitter := servicesSnapshotEmitter()
	filter := func(event serviceStatusChangeEvent) bool {
		col := event.Namespace.Collection
		return col == shared.ServicesCollection || col == shared.DeploysCollection
	}
	collections := []string{
		shared.ServicesCollection,
		shared.DeploysCollection,
	}

	streamSSE(c, snapshot, collections, filter, emitter, "services-overview")
}

// --- Generic SSE stream engine ------------------------------------------------

func streamSSE(
	c *gin.Context,
	initialSnapshot interface{},
	collections []string,
	filter changeEventFilter,
	emitter snapshotEmitter,
	label string,
) {
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	if _, ok := c.Writer.(http.Flusher); !ok {
		shared.RespondError(c, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	if err := writeSSEEvent(c, "snapshot", initialSnapshot); err != nil {
		return
	}
	lastDigest, err := snapshotDigest(initialSnapshot)
	if err != nil {
		return
	}

	stream, err := openChangeStream(c.Request.Context(), collections)
	if err != nil {
		log.Printf("[sse] change stream unavailable for %s, using polling: %v", label, err)
		pollSnapshots(c, emitter, lastDigest, label)
		return
	}
	defer stream.Close(c.Request.Context())

	streamWithChangeStream(c, stream, filter, emitter, lastDigest, label)
}

func streamWithChangeStream(
	c *gin.Context,
	stream *mongo.ChangeStream,
	filter changeEventFilter,
	emitter snapshotEmitter,
	lastDigest string,
	label string,
) {
	heartbeat := time.NewTicker(statusStreamHeartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			if err := writeSSEComment(c, "ping"); err != nil {
				return
			}
		default:
		}

		if !stream.TryNext(c.Request.Context()) {
			if err := stream.Err(); err != nil {
				log.Printf("[sse] change stream failed for %s, switching to polling: %v", label, err)
				pollSnapshots(c, emitter, lastDigest, label)
				return
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}

		var event serviceStatusChangeEvent
		if err := stream.Decode(&event); err != nil {
			log.Printf("[sse] decode error for %s: %v", label, err)
			continue
		}
		if !filter(event) {
			continue
		}

		nextDigest, keepOpen, err := emitter(c, lastDigest)
		if err != nil {
			log.Printf("[sse] emit error for %s: %v", label, err)
			continue
		}
		lastDigest = nextDigest
		if !keepOpen {
			return
		}
	}
}

func pollSnapshots(c *gin.Context, emitter snapshotEmitter, lastDigest, label string) {
	updates := time.NewTicker(statusStreamPollInterval())
	heartbeat := time.NewTicker(statusStreamHeartbeatInterval)
	defer updates.Stop()
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-heartbeat.C:
			if err := writeSSEComment(c, "ping"); err != nil {
				return
			}
		case <-updates.C:
			nextDigest, keepOpen, err := emitter(c, lastDigest)
			if err != nil {
				log.Printf("[sse] poll error for %s: %v", label, err)
				continue
			}
			lastDigest = nextDigest
			if !keepOpen {
				return
			}
		}
	}
}

// --- Snapshot loaders ---------------------------------------------------------

func loadServiceStatusSnapshot(ctx context.Context, serviceID, environment string) (serviceStatusSnapshot, bool, error) {
	service, err := shared.FindOne(ctx, shared.Collection(shared.ServicesCollection), bson.M{"id": serviceID})
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return serviceStatusSnapshot{}, false, nil
		}
		return serviceStatusSnapshot{}, false, err
	}

	scopedFilter := bson.M{"serviceId": serviceID}
	if environment != "" {
		scopedFilter["environment"] = environment
	}

	deploys, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), scopedFilter)
	if err != nil {
		return serviceStatusSnapshot{}, false, err
	}
	operations.NormalizeDeployDocuments(deploys)
	rules, err := shared.FindAll(ctx, shared.Collection(shared.RulesCollection), scopedFilter)
	if err != nil {
		return serviceStatusSnapshot{}, false, err
	}
	ruleDeploys, err := shared.FindAll(ctx, shared.Collection(shared.RuleDeploysCollection), scopedFilter)
	if err != nil {
		return serviceStatusSnapshot{}, false, err
	}

	return serviceStatusSnapshot{
		Service:     service,
		Deploys:     normalizeSlice(deploys),
		Rules:       normalizeSlice(rules),
		RuleDeploys: normalizeSlice(ruleDeploys),
		EmittedAt:   shared.NowISO(),
	}, true, nil
}

func loadServicesStatusSnapshot(ctx context.Context) (servicesStatusSnapshot, error) {
	servicesList, err := shared.FindAll(ctx, shared.Collection(shared.ServicesCollection), bson.M{})
	if err != nil {
		return servicesStatusSnapshot{}, err
	}
	deploys, err := shared.FindAll(ctx, shared.Collection(shared.DeploysCollection), bson.M{})
	if err != nil {
		return servicesStatusSnapshot{}, err
	}
	operations.NormalizeDeployDocuments(deploys)
	return servicesStatusSnapshot{
		Services:  normalizeSlice(servicesList),
		Deploys:   normalizeSlice(deploys),
		EmittedAt: shared.NowISO(),
	}, nil
}

// --- Emitters (adapters between loaders and the generic engine) ---------------

func serviceSnapshotEmitter(serviceID, environment string) snapshotEmitter {
	return func(c *gin.Context, currentDigest string) (string, bool, error) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
		defer cancel()

		snapshot, found, err := loadServiceStatusSnapshot(ctx, serviceID, environment)
		if err != nil {
			return currentDigest, true, err
		}
		if !found {
			_ = writeSSEEvent(c, "deleted", gin.H{"serviceId": serviceID})
			return currentDigest, false, nil
		}
		return emitIfChanged(c, snapshot, currentDigest)
	}
}

func servicesSnapshotEmitter() snapshotEmitter {
	return func(c *gin.Context, currentDigest string) (string, bool, error) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), shared.DBTimeout)
		defer cancel()

		snapshot, err := loadServicesStatusSnapshot(ctx)
		if err != nil {
			return currentDigest, true, err
		}
		return emitIfChanged(c, snapshot, currentDigest)
	}
}

func emitIfChanged(c *gin.Context, snapshot interface{}, currentDigest string) (string, bool, error) {
	nextDigest, err := snapshotDigest(snapshot)
	if err != nil {
		return currentDigest, true, err
	}
	if nextDigest == currentDigest {
		return currentDigest, true, nil
	}
	if err := writeSSEEvent(c, "snapshot", snapshot); err != nil {
		return currentDigest, false, err
	}
	return nextDigest, true, nil
}

// --- Change stream helpers ----------------------------------------------------

func openChangeStream(ctx context.Context, collections []string) (*mongo.ChangeStream, error) {
	collValues := make(bson.A, len(collections))
	for i, c := range collections {
		collValues[i] = c
	}
	pipeline := mongo.Pipeline{
		bson.D{{Key: "$match", Value: bson.D{
			{Key: "ns.coll", Value: bson.D{{Key: "$in", Value: collValues}}},
			{Key: "operationType", Value: bson.D{{Key: "$in", Value: bson.A{"insert", "update", "replace", "delete"}}}},
		}}},
	}
	return client.Mongo().Database(client.DBName).Watch(
		ctx,
		pipeline,
		options.ChangeStream().SetFullDocument(options.UpdateLookup),
	)
}

func isServiceStatusEventRelevant(event serviceStatusChangeEvent, serviceID string) bool {
	switch event.Namespace.Collection {
	case shared.ServicesCollection:
		return matchesServiceID(event.FullDocument, event.DocumentKey, serviceID)
	case shared.DeploysCollection, shared.RulesCollection, shared.RuleDeploysCollection:
		sid := strings.TrimSpace(shared.StringValue(event.FullDocument["serviceId"]))
		if sid != "" {
			return sid == serviceID
		}
		return event.OperationType == "delete"
	default:
		return false
	}
}

func matchesServiceID(fullDocument, documentKey bson.M, serviceID string) bool {
	for _, candidate := range []string{
		strings.TrimSpace(shared.StringValue(fullDocument["id"])),
		strings.TrimSpace(shared.StringValue(fullDocument["_id"])),
		strings.TrimSpace(shared.StringValue(documentKey["_id"])),
	} {
		if candidate != "" && candidate == serviceID {
			return true
		}
	}
	return false
}

// --- SSE wire helpers ---------------------------------------------------------

func snapshotDigest(payload interface{}) (string, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	hash := sha1.Sum(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func writeSSEEvent(c *gin.Context, event string, payload interface{}) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, data); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func writeSSEComment(c *gin.Context, comment string) error {
	if _, err := fmt.Fprintf(c.Writer, ": %s\n\n", comment); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

func normalizeSlice(items []bson.M) []bson.M {
	if items == nil {
		return []bson.M{}
	}
	return items
}

func statusStreamPollInterval() time.Duration {
	raw := strings.TrimSpace(os.Getenv("STATUS_STREAM_POLL_SECONDS"))
	if raw == "" {
		return defaultStatusStreamPollInterval
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return defaultStatusStreamPollInterval
	}
	interval := time.Duration(value) * time.Second
	if interval < minStatusStreamPollInterval {
		return minStatusStreamPollInterval
	}
	if interval > maxStatusStreamPollInterval {
		return maxStatusStreamPollInterval
	}
	return interval
}
