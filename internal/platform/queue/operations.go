package queue

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/url"
	"os"
	"strings"
	"time"

	platformlogging "releaseaapi/internal/platform/logging"
	"releaseaapi/internal/platform/shared"
	platformutils "releaseaapi/internal/platform/utils"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/bson"
)

type operationMessage struct {
	OperationID   string `json:"operationId"`
	CorrelationID string `json:"correlationId,omitempty"`
}

const (
	defaultWorkerQueueName = "releasea.worker"
	dispatchBrokerName     = "rabbitmq"
	dispatchStatusSending  = "dispatching"
	dispatchStatusSent     = "dispatched"
	dispatchStatusFailed   = "dispatch-failed"
)

type queueTopology struct {
	QueueName           string
	DeadLetterEnabled   bool
	DeadLetterQueueName string
	MainQueueArgs       amqp.Table
}

func PublishOperation(ctx context.Context, operationID string) error {
	rabbitURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if rabbitURL == "" {
		return errors.New("RABBITMQ_URL not configured")
	}
	correlationID := shared.CorrelationIDFromContext(ctx)
	if correlationID == "" {
		correlationID = shared.NewCorrelationID()
		ctx = shared.WithCorrelationID(ctx, correlationID)
	}

	queueName := resolveWorkerQueueName()
	topology := resolveQueueTopology(queueName)

	if err := markOperationDispatchAttempt(ctx, operationID, topology, correlationID); err != nil {
		return err
	}

	platformlogging.LogInfo("queue.operation.publish", platformlogging.LogFields{
		"operationId":   operationID,
		"queue":         queueName,
		"correlationId": correlationID,
	})

	conn, err := dialRabbit(rabbitURL)
	if err != nil {
		return err
	}
	defer conn.Close()

	ch, err := conn.Channel()
	if err != nil {
		return err
	}
	defer ch.Close()

	if err := ch.Confirm(false); err != nil {
		return err
	}
	confirmations := ch.NotifyPublish(make(chan amqp.Confirmation, 1))

	if err := declareQueueTopology(ch, topology); err != nil {
		return err
	}

	payload, err := json.Marshal(operationMessage{OperationID: operationID, CorrelationID: correlationID})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := ch.PublishWithContext(
		ctx,
		"",
		queueName,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         payload,
			DeliveryMode: amqp.Persistent,
			Headers: amqp.Table{
				"x-correlation-id": correlationID,
			},
		},
	); err != nil {
		return err
	}

	confirmation, err := waitForBrokerConfirmation(ctx, confirmations)
	if err != nil {
		return err
	}

	return markOperationDispatched(ctx, operationID, topology, confirmation, correlationID)
}

func PublishOperationWithDispatchError(ctx context.Context, operationID string) {
	if err := PublishOperation(ctx, operationID); err != nil {
		RecordOperationDispatchError(ctx, operationID, err)
	}
}

func RecordOperationDispatchError(ctx context.Context, operationID string, err error) {
	if err == nil {
		return
	}
	now := shared.NowISO()
	_, _ = shared.Collection(shared.OperationsCollection).UpdateOne(
		ctx,
		bson.M{"_id": operationID},
		bson.M{
			"$set": bson.M{
				"dispatch.status":      dispatchStatusFailed,
				"dispatch.broker":      dispatchBrokerName,
				"dispatch.error":       err.Error(),
				"dispatch.lastErrorAt": now,
				"dispatch.updatedAt":   now,
				"dispatchError":        err.Error(),
				"updatedAt":            now,
			},
		},
	)
	recordOperationDispatchAudit(ctx, operationID, "operation.dispatch.failed", "failed", map[string]interface{}{
		"error": err.Error(),
	})
}

func resolveWorkerQueueName() string {
	queueName := strings.TrimSpace(os.Getenv("WORKER_QUEUE"))
	if queueName == "" {
		return defaultWorkerQueueName
	}
	return queueName
}

func resolveQueueTopology(queueName string) queueTopology {
	topology := queueTopology{
		QueueName:         queueName,
		DeadLetterEnabled: platformutils.EnvBool("WORKER_QUEUE_DLQ_ENABLE", true),
	}
	if !topology.DeadLetterEnabled {
		return topology
	}

	dlqName := strings.TrimSpace(os.Getenv("WORKER_QUEUE_DLQ_NAME"))
	if dlqName == "" {
		dlqName = queueName + ".dead-letter"
	}
	topology.DeadLetterQueueName = dlqName
	topology.MainQueueArgs = amqp.Table{
		"x-dead-letter-exchange":    "",
		"x-dead-letter-routing-key": dlqName,
	}
	return topology
}

func declareQueueTopology(ch *amqp.Channel, topology queueTopology) error {
	if topology.DeadLetterEnabled {
		if _, err := ch.QueueDeclare(
			topology.DeadLetterQueueName,
			true,
			false,
			false,
			false,
			nil,
		); err != nil {
			return err
		}
	}

	_, err := ch.QueueDeclare(
		topology.QueueName,
		true,
		false,
		false,
		false,
		topology.MainQueueArgs,
	)
	return err
}

func markOperationDispatchAttempt(ctx context.Context, operationID string, topology queueTopology, correlationID string) error {
	now := shared.NowISO()
	_, err := shared.Collection(shared.OperationsCollection).UpdateOne(
		ctx,
		bson.M{"_id": operationID},
		bson.M{
			"$set": bson.M{
				"dispatch.status":        dispatchStatusSending,
				"dispatch.broker":        dispatchBrokerName,
				"dispatch.queue":         topology.QueueName,
				"dispatch.deadLetter":    topology.DeadLetterEnabled,
				"dispatch.correlationId": correlationID,
				"dispatch.lastAttemptAt": now,
				"dispatch.updatedAt":     now,
				"updatedAt":              now,
			},
			"$inc": bson.M{
				"dispatch.attempts": 1,
			},
			"$unset": bson.M{
				"dispatch.error":       "",
				"dispatch.lastErrorAt": "",
				"dispatchError":        "",
			},
		},
	)
	if err != nil {
		return err
	}
	if topology.DeadLetterEnabled {
		_, err = shared.Collection(shared.OperationsCollection).UpdateOne(
			ctx,
			bson.M{"_id": operationID},
			bson.M{
				"$set": bson.M{
					"dispatch.deadLetterQueue": topology.DeadLetterQueueName,
				},
			},
		)
	}
	return err
}

func waitForBrokerConfirmation(ctx context.Context, confirmations <-chan amqp.Confirmation) (amqp.Confirmation, error) {
	select {
	case <-ctx.Done():
		return amqp.Confirmation{}, ctx.Err()
	case confirmation, ok := <-confirmations:
		if !ok {
			return amqp.Confirmation{}, errors.New("rabbitmq confirmation channel closed")
		}
		if !confirmation.Ack {
			return amqp.Confirmation{}, errors.New("rabbitmq publish was nacked by broker")
		}
		return confirmation, nil
	}
}

func markOperationDispatched(ctx context.Context, operationID string, topology queueTopology, confirmation amqp.Confirmation, correlationID string) error {
	now := shared.NowISO()
	_, err := shared.Collection(shared.OperationsCollection).UpdateOne(
		ctx,
		bson.M{"_id": operationID},
		bson.M{
			"$set": bson.M{
				"dispatch.status":           dispatchStatusSent,
				"dispatch.broker":           dispatchBrokerName,
				"dispatch.queue":            topology.QueueName,
				"dispatch.correlationId":    correlationID,
				"dispatch.deadLetter":       topology.DeadLetterEnabled,
				"dispatch.deadLetterQueue":  topology.DeadLetterQueueName,
				"dispatch.lastConfirmedAt":  now,
				"dispatch.lastConfirmation": "ack",
				"dispatch.lastDeliveryTag":  confirmation.DeliveryTag,
				"dispatch.updatedAt":        now,
				"updatedAt":                 now,
			},
			"$unset": bson.M{
				"dispatch.error":       "",
				"dispatch.lastErrorAt": "",
				"dispatchError":        "",
			},
		},
	)
	if err == nil {
		recordOperationDispatchAudit(ctx, operationID, "operation.dispatch.confirmed", "success", map[string]interface{}{
			"deliveryTag":   confirmation.DeliveryTag,
			"correlationId": correlationID,
		})
	}
	return err
}

func recordOperationDispatchAudit(ctx context.Context, operationID, action, status string, metadata map[string]interface{}) {
	if strings.TrimSpace(operationID) == "" {
		return
	}
	if metadata == nil {
		metadata = map[string]interface{}{}
	}

	op, err := shared.FindOne(ctx, shared.Collection(shared.OperationsCollection), bson.M{"id": operationID})
	if err == nil {
		if operationType := strings.TrimSpace(shared.StringValue(op["type"])); operationType != "" {
			metadata["operationType"] = operationType
		}
		if resourceType := strings.TrimSpace(shared.StringValue(op["resourceType"])); resourceType != "" {
			metadata["targetResourceType"] = resourceType
		}
		if resourceID := strings.TrimSpace(shared.StringValue(op["resourceId"])); resourceID != "" {
			metadata["targetResourceId"] = resourceID
		}
		if dispatch := shared.MapPayload(op["dispatch"]); len(dispatch) > 0 {
			if queueName := strings.TrimSpace(shared.StringValue(dispatch["queue"])); queueName != "" {
				metadata["queue"] = queueName
			}
			if deadLetterQueue := strings.TrimSpace(shared.StringValue(dispatch["deadLetterQueue"])); deadLetterQueue != "" {
				metadata["deadLetterQueue"] = deadLetterQueue
			}
			if correlationID := strings.TrimSpace(shared.StringValue(dispatch["correlationId"])); correlationID != "" {
				metadata["correlationId"] = correlationID
			}
			if attempts := shared.IntValue(dispatch["attempts"]); attempts > 0 {
				metadata["attempts"] = attempts
			}
		}
	}

	shared.RecordAuditEvent(ctx, shared.AuditEvent{
		Action:       action,
		ResourceType: "operation",
		ResourceID:   operationID,
		Status:       status,
		Source:       "queue",
		Metadata:     metadata,
	})
}

func dialRabbit(rabbitURL string) (*amqp.Connection, error) {
	tlsConfig, err := rabbitTLSConfig(rabbitURL)
	if err != nil {
		return nil, err
	}
	if tlsConfig != nil {
		return amqp.DialConfig(rabbitURL, amqp.Config{TLSClientConfig: tlsConfig})
	}
	return amqp.Dial(rabbitURL)
}

func rabbitTLSConfig(rabbitURL string) (*tls.Config, error) {
	enableTLS, err := rabbitTLSEnabled(rabbitURL)
	if err != nil {
		return nil, err
	}
	serverName := strings.TrimSpace(os.Getenv("RABBITMQ_TLS_SERVER_NAME"))
	if parsed, err := url.Parse(rabbitURL); err == nil {
		if serverName == "" {
			serverName = parsed.Hostname()
		}
	}
	if !enableTLS {
		return nil, nil
	}

	caPath := strings.TrimSpace(os.Getenv("RABBITMQ_TLS_CA_PATH"))
	certPath := strings.TrimSpace(os.Getenv("RABBITMQ_TLS_CERT_PATH"))
	keyPath := strings.TrimSpace(os.Getenv("RABBITMQ_TLS_KEY_PATH"))
	insecure := platformutils.EnvBool("RABBITMQ_TLS_INSECURE", false)
	if insecure && rabbitTLSRequired() && !platformutils.EnvBool("RABBITMQ_ALLOW_INSECURE_IN_PRODUCTION", false) {
		return nil, errors.New("RABBITMQ_TLS_INSECURE is not allowed when production queue TLS is required")
	}

	tlsConfig := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: insecure,
	}
	if serverName != "" {
		tlsConfig.ServerName = serverName
	}
	if caPath != "" {
		caBytes, err := os.ReadFile(caPath)
		if err != nil {
			return nil, err
		}
		rootCAs := x509.NewCertPool()
		if !rootCAs.AppendCertsFromPEM(caBytes) {
			return nil, errors.New("failed to parse RABBITMQ_TLS_CA_PATH")
		}
		tlsConfig.RootCAs = rootCAs
	}
	if certPath != "" || keyPath != "" {
		if certPath == "" || keyPath == "" {
			return nil, errors.New("RABBITMQ_TLS_CERT_PATH and RABBITMQ_TLS_KEY_PATH must both be set")
		}
		cert, err := tls.LoadX509KeyPair(certPath, keyPath)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}

func rabbitTLSEnabled(rabbitURL string) (bool, error) {
	enableTLS := platformutils.EnvBool("RABBITMQ_TLS_ENABLE", false)
	parsed, _ := url.Parse(rabbitURL)
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme == "amqps" {
		enableTLS = true
	}

	switch strings.ToLower(strings.TrimSpace(os.Getenv("RABBITMQ_TLS_MODE"))) {
	case "", "auto":
		if rabbitTLSRequired() && !enableTLS {
			return false, errors.New("production queue transport requires TLS; configure amqps:// or enable RabbitMQ TLS explicitly")
		}
		return enableTLS, nil
	case "required":
		if !enableTLS {
			return false, errors.New("RABBITMQ_TLS_MODE=required requires TLS-enabled RabbitMQ transport")
		}
		return true, nil
	case "disabled":
		if rabbitTLSRequired() && !platformutils.EnvBool("RABBITMQ_ALLOW_INSECURE_IN_PRODUCTION", false) {
			return false, errors.New("RABBITMQ_TLS_MODE=disabled is not allowed in production without RABBITMQ_ALLOW_INSECURE_IN_PRODUCTION=true")
		}
		return false, nil
	default:
		return false, errors.New("RABBITMQ_TLS_MODE must be one of auto, required, or disabled")
	}
}

func rabbitTLSRequired() bool {
	if platformutils.EnvBool("RABBITMQ_TLS_REQUIRE", false) {
		return true
	}
	for _, key := range []string{"RELEASEA_RUNTIME_ENV", "RELEASEA_ENV", "APP_ENV", "ENVIRONMENT"} {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
		case "prod", "production":
			return true
		}
	}
	return false
}
