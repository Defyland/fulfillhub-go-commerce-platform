package messaging

import (
	"context"
	"strings"
	"testing"

	amqp "github.com/rabbitmq/amqp091-go"
)

func TestWaitForAMQPPublishOutcomeAcceptsBrokerAck(t *testing.T) {
	confirms := make(chan amqp.Confirmation, 1)
	returns := make(chan amqp.Return, 1)
	confirms <- amqp.Confirmation{Ack: true}

	if err := waitForAMQPPublishOutcome(context.Background(), confirms, returns, "msg_1"); err != nil {
		t.Fatalf("waitForAMQPPublishOutcome returned error: %v", err)
	}
}

func TestWaitForAMQPPublishOutcomeRejectsBrokerNack(t *testing.T) {
	confirms := make(chan amqp.Confirmation, 1)
	returns := make(chan amqp.Return, 1)
	confirms <- amqp.Confirmation{Ack: false}

	err := waitForAMQPPublishOutcome(context.Background(), confirms, returns, "msg_1")
	if err == nil || !strings.Contains(err.Error(), "negatively acknowledged") {
		t.Fatalf("error = %v, want negative acknowledgement", err)
	}
}

func TestWaitForAMQPPublishOutcomeRejectsReturnedMessage(t *testing.T) {
	confirms := make(chan amqp.Confirmation, 1)
	returns := make(chan amqp.Return, 1)
	returns <- amqp.Return{MessageId: "msg_1", ReplyText: "NO_ROUTE"}

	err := waitForAMQPPublishOutcome(context.Background(), confirms, returns, "msg_1")
	if err == nil || !strings.Contains(err.Error(), "unroutable") {
		t.Fatalf("error = %v, want unroutable return", err)
	}
}
