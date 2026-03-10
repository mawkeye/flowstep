// Package redisbus provides a Redis Streams implementation of the flowstep EventBus.
// Uses XADD to emit events and XREADGROUP for consumer group subscriptions.
package redisbus

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/mawkeye/flowstep/types"
)

// Bus implements flowstep.EventBus using Redis Streams.
type Bus struct {
	client     *redis.Client
	streamName string
}

// New creates a new Redis Streams EventBus.
func New(client *redis.Client, streamName string) *Bus {
	return &Bus{
		client:     client,
		streamName: streamName,
	}
}

// Emit publishes an event to the Redis Stream via XADD.
func (b *Bus) Emit(ctx context.Context, event types.DomainEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("redisbus: marshal event: %w", err)
	}

	_, err = b.client.XAdd(ctx, &redis.XAddArgs{
		Stream: b.streamName,
		Values: map[string]any{
			"event_id":       event.ID,
			"event_type":     event.EventType,
			"aggregate_type": event.AggregateType,
			"aggregate_id":   event.AggregateID,
			"correlation_id": event.CorrelationID,
			"data":           string(data),
		},
	}).Result()
	if err != nil {
		return fmt.Errorf("redisbus: xadd: %w", err)
	}
	return nil
}

// Handler processes events from the stream.
type Handler func(ctx context.Context, event types.DomainEvent) error

// Subscribe reads events from a consumer group. Blocks until context is cancelled.
func (b *Bus) Subscribe(ctx context.Context, group, consumer string, handler Handler) error {
	// Create consumer group if it doesn't exist
	_ = b.client.XGroupCreateMkStream(ctx, b.streamName, group, "0").Err()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		streams, err := b.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group:    group,
			Consumer: consumer,
			Streams:  []string{b.streamName, ">"},
			Count:    10,
			Block:    0,
		}).Result()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("redisbus: xreadgroup: %w", err)
		}

		for _, stream := range streams {
			for _, msg := range stream.Messages {
				data, ok := msg.Values["data"].(string)
				if !ok {
					continue
				}
				var event types.DomainEvent
				if err := json.Unmarshal([]byte(data), &event); err != nil {
					continue
				}
				if err := handler(ctx, event); err != nil {
					continue
				}
				b.client.XAck(ctx, b.streamName, group, msg.ID)
			}
		}
	}
}
