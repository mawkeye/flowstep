// Package natsbus provides a NATS JetStream implementation of the flowstep EventBus.
// Uses Publish on Emit and durable consumers for subscriptions.
package natsbus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"github.com/mawkeye/flowstep/types"
)

// Bus implements flowstep.EventBus using NATS JetStream.
type Bus struct {
	js      nats.JetStreamContext
	subject string
}

// New creates a new NATS JetStream EventBus.
// The subject is the NATS subject to publish/subscribe on (e.g., "flowstep.events").
func New(js nats.JetStreamContext, subject string) *Bus {
	return &Bus{
		js:      js,
		subject: subject,
	}
}

// Emit publishes an event to the NATS JetStream subject.
func (b *Bus) Emit(_ context.Context, event types.DomainEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("natsbus: marshal event: %w", err)
	}

	_, err = b.js.Publish(b.subject, data)
	if err != nil {
		return fmt.Errorf("natsbus: publish: %w", err)
	}
	return nil
}

// Handler processes events from the stream.
type Handler func(ctx context.Context, event types.DomainEvent) error

// Subscribe creates a durable consumer and processes events. Blocks until context is cancelled.
func (b *Bus) Subscribe(ctx context.Context, durableName string, handler Handler) error {
	sub, err := b.js.Subscribe(b.subject, func(msg *nats.Msg) {
		var event types.DomainEvent
		if err := json.Unmarshal(msg.Data, &event); err != nil {
			_ = msg.Nak()
			return
		}
		if err := handler(ctx, event); err != nil {
			_ = msg.Nak()
			return
		}
		_ = msg.Ack()
	}, nats.Durable(durableName), nats.ManualAck())
	if err != nil {
		return fmt.Errorf("natsbus: subscribe: %w", err)
	}
	defer sub.Unsubscribe()

	<-ctx.Done()
	return ctx.Err()
}
