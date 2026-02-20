package shared

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/bson"
)

type operationMessage struct {
	OperationID string `json:"operationId"`
}

func PublishOperation(ctx context.Context, operationID string) error {
	rabbitURL := strings.TrimSpace(os.Getenv("RABBITMQ_URL"))
	if rabbitURL == "" {
		return errors.New("RABBITMQ_URL not configured")
	}

	queueName := strings.TrimSpace(os.Getenv("WORKER_QUEUE"))
	if queueName == "" {
		queueName = "releasea.worker"
	}

	log.Printf("[queue] publishing operation=%s queue=%s", operationID, queueName)

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

	_, err = ch.QueueDeclare(
		queueName,
		true,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		return err
	}

	payload, err := json.Marshal(operationMessage{OperationID: operationID})
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return ch.PublishWithContext(
		ctx,
		"",
		queueName,
		false,
		false,
		amqp.Publishing{
			ContentType:  "application/json",
			Body:         payload,
			DeliveryMode: amqp.Persistent,
		},
	)
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
	_ = UpdateByID(ctx, Collection(OperationsCollection), operationID, bson.M{
		"dispatchError": err.Error(),
		"updatedAt":     NowISO(),
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
	enableTLS := EnvBool("RABBITMQ_TLS_ENABLE", false)
	serverName := strings.TrimSpace(os.Getenv("RABBITMQ_TLS_SERVER_NAME"))
	if parsed, err := url.Parse(rabbitURL); err == nil {
		if parsed.Scheme == "amqps" {
			enableTLS = true
		}
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
	insecure := EnvBool("RABBITMQ_TLS_INSECURE", false)

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
