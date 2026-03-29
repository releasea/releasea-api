package queue

import (
	"context"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestResolveWorkerQueueNameUsesDefault(t *testing.T) {
	t.Setenv("WORKER_QUEUE", "")

	if got := resolveWorkerQueueName(); got != defaultWorkerQueueName {
		t.Fatalf("expected default queue %q, got %q", defaultWorkerQueueName, got)
	}
}

func TestResolveWorkerQueueNameUsesConfiguredValue(t *testing.T) {
	t.Setenv("WORKER_QUEUE", "custom.worker.queue")

	if got := resolveWorkerQueueName(); got != "custom.worker.queue" {
		t.Fatalf("expected configured queue, got %q", got)
	}
}

func TestResolveQueueTopologyDefaults(t *testing.T) {
	topology := resolveQueueTopology("releasea.worker")

	if !topology.DeadLetterEnabled {
		t.Fatalf("expected DLQ enabled by default")
	}
	if topology.DeadLetterQueueName != "releasea.worker.dead-letter" {
		t.Fatalf("unexpected default DLQ name: %q", topology.DeadLetterQueueName)
	}
	if topology.MainQueueArgs["x-dead-letter-routing-key"] != "releasea.worker.dead-letter" {
		t.Fatalf("unexpected DLQ routing key: %#v", topology.MainQueueArgs)
	}
}

func TestResolveQueueTopologyCanDisableDLQ(t *testing.T) {
	t.Setenv("WORKER_QUEUE_DLQ_ENABLE", "false")

	topology := resolveQueueTopology("releasea.worker")
	if topology.DeadLetterEnabled {
		t.Fatalf("expected DLQ disabled")
	}
	if topology.DeadLetterQueueName != "" {
		t.Fatalf("expected empty DLQ name, got %q", topology.DeadLetterQueueName)
	}
	if topology.MainQueueArgs != nil {
		t.Fatalf("expected no queue args when DLQ disabled")
	}
}

func TestWaitForBrokerConfirmationAck(t *testing.T) {
	confirmations := make(chan amqp.Confirmation, 1)
	confirmations <- amqp.Confirmation{Ack: true, DeliveryTag: 42}

	confirmation, err := waitForBrokerConfirmation(context.Background(), confirmations)
	if err != nil {
		t.Fatalf("expected ack confirmation, got error: %v", err)
	}
	if confirmation.DeliveryTag != 42 {
		t.Fatalf("expected delivery tag 42, got %d", confirmation.DeliveryTag)
	}
}

func TestWaitForBrokerConfirmationNack(t *testing.T) {
	confirmations := make(chan amqp.Confirmation, 1)
	confirmations <- amqp.Confirmation{Ack: false}

	if _, err := waitForBrokerConfirmation(context.Background(), confirmations); err == nil {
		t.Fatalf("expected nack error")
	}
}

func TestWaitForBrokerConfirmationContextTimeout(t *testing.T) {
	confirmations := make(chan amqp.Confirmation)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	if _, err := waitForBrokerConfirmation(ctx, confirmations); err == nil {
		t.Fatalf("expected context timeout error")
	}
}

func TestRabbitTLSEnabledRequiresTLSInProduction(t *testing.T) {
	t.Setenv("RELEASEA_RUNTIME_ENV", "production")
	t.Setenv("RABBITMQ_TLS_MODE", "auto")
	t.Setenv("RABBITMQ_TLS_ENABLE", "false")

	if _, err := rabbitTLSEnabled("amqp://guest:guest@rabbitmq:5672/"); err == nil {
		t.Fatalf("expected production TLS requirement error")
	}
}

func TestRabbitTLSEnabledRejectsDisabledModeInProduction(t *testing.T) {
	t.Setenv("RELEASEA_RUNTIME_ENV", "production")
	t.Setenv("RABBITMQ_TLS_MODE", "disabled")

	if _, err := rabbitTLSEnabled("amqp://guest:guest@rabbitmq:5672/"); err == nil {
		t.Fatalf("expected disabled mode rejection in production")
	}
}

func TestRabbitTLSEnabledAcceptsAmqpsWhenRequired(t *testing.T) {
	t.Setenv("RABBITMQ_TLS_MODE", "required")

	enabled, err := rabbitTLSEnabled("amqps://guest:guest@rabbitmq:5671/")
	if err != nil {
		t.Fatalf("unexpected TLS mode error: %v", err)
	}
	if !enabled {
		t.Fatalf("expected TLS to be enabled")
	}
}
