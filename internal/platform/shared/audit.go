package shared

import (
	"context"
	"log"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
)

type AuditEvent struct {
	Action       string
	ResourceType string
	ResourceID   string
	Status       string
	ActorID      string
	ActorName    string
	ActorRole    string
	Source       string
	Message      string
	Metadata     map[string]interface{}
}

func RecordAuditEvent(ctx context.Context, event AuditEvent) {
	if strings.TrimSpace(event.Action) == "" {
		return
	}
	eventID := "audit-" + uuid.NewString()
	if strings.TrimSpace(event.Status) == "" {
		event.Status = "success"
	}
	if strings.TrimSpace(event.Source) == "" {
		event.Source = "api"
	}
	if event.Metadata == nil {
		event.Metadata = map[string]interface{}{}
	}

	doc := bson.M{
		"_id":          eventID,
		"id":           eventID,
		"action":       event.Action,
		"resourceType": event.ResourceType,
		"resourceId":   event.ResourceID,
		"status":       event.Status,
		"actor": bson.M{
			"id":   event.ActorID,
			"name": event.ActorName,
			"role": event.ActorRole,
		},
		"source":    event.Source,
		"message":   event.Message,
		"metadata":  event.Metadata,
		"createdAt": NowISO(),
	}
	if err := InsertOne(ctx, Collection(PlatformAuditCollection), doc); err != nil {
		log.Printf("[audit] failed to record event action=%s resource=%s/%s: %v", event.Action, event.ResourceType, event.ResourceID, err)
	}
}

func AuditActorFromContext(c *gin.Context) (actorID, actorName, actorRole string) {
	if c == nil {
		return "", "", ""
	}
	if value, ok := c.Get("authUserId"); ok {
		actorID, _ = value.(string)
	}
	if value, ok := c.Get("authRole"); ok {
		actorRole, _ = value.(string)
	}
	actorName = AuthDisplayName(c)
	return strings.TrimSpace(actorID), strings.TrimSpace(actorName), strings.TrimSpace(actorRole)
}
