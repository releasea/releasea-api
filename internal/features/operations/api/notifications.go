package operations

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	httpclient "releaseaapi/internal/platform/http/client"
	httpheaders "releaseaapi/internal/platform/http/headers"
	"releaseaapi/internal/platform/shared"

	"go.mongodb.org/mongo-driver/bson"
)

func notifyOperationResult(ctx context.Context, op bson.M, status, message string) {
	status = strings.TrimSpace(strings.ToLower(status))
	if status != StatusSucceeded && status != StatusFailed {
		return
	}

	settings, err := shared.FindOne(ctx, shared.Collection(shared.PlatformSettingsCollection), bson.M{})
	if err != nil || settings == nil {
		return
	}
	notifications := shared.MapPayload(settings["notifications"])
	if status == StatusSucceeded && !shared.BoolValue(notifications["deploySuccess"]) {
		return
	}
	if status == StatusFailed && !shared.BoolValue(notifications["deployFailed"]) {
		return
	}

	webhookURL := resolveNotificationWebhook(settings, notifications)
	if webhookURL == "" {
		return
	}

	payload := map[string]interface{}{
		"event":       "operation." + status,
		"status":      status,
		"message":     message,
		"operation":   shared.StringValue(op["id"]),
		"type":        shared.StringValue(op["type"]),
		"resourceId":  shared.StringValue(op["resourceId"]),
		"serviceName": shared.StringValue(op["serviceName"]),
		"requestedBy": shared.StringValue(op["requestedBy"]),
		"timestamp":   shared.NowISO(),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}

	go func(url string, body []byte) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
		if err != nil {
			return
		}
		httpheaders.SetContentTypeJSON(req)
		client := httpclient.New(5 * time.Second)
		resp, err := client.Do(req)
		if err != nil {
			return
		}
		resp.Body.Close()
	}(webhookURL, raw)
}

func resolveNotificationWebhook(settings bson.M, notifications map[string]interface{}) string {
	if url := strings.TrimSpace(shared.StringValue(settings["notificationWebhookUrl"])); url != "" {
		return url
	}
	if url := strings.TrimSpace(shared.StringValue(notifications["webhookUrl"])); url != "" {
		return url
	}
	if url := strings.TrimSpace(os.Getenv("NOTIFICATIONS_WEBHOOK_URL")); url != "" {
		return url
	}
	if url := strings.TrimSpace(os.Getenv("RELEASEA_NOTIFICATIONS_WEBHOOK_URL")); url != "" {
		return url
	}
	return ""
}
