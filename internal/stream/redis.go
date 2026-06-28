package stream

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/0xLaiHo/rc_tobias/internal/outbox"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type Publisher struct {
	client *redis.Client
	stream string
}

// NewPublisher publishes delivery-request messages. The message payload is kept
// intentionally small so PostgreSQL stays the source of truth.
func NewPublisher(client *redis.Client, stream string) *Publisher {
	if stream == "" {
		stream = DefaultStream
	}
	return &Publisher{client: client, stream: stream}
}

// Publish writes one delivery event to Redis Streams and returns Redis' stream
// ID for audit/debugging in the outbox table.
func (p *Publisher) Publish(ctx context.Context, event outbox.Event) (string, error) {
	return p.client.XAdd(ctx, &redis.XAddArgs{
		Stream: p.stream,
		Values: BuildDeliveryMessage(event),
	}).Result()
}

type Processor interface {
	Process(ctx context.Context, notificationID uuid.UUID) error
}

type Consumer struct {
	client    *redis.Client
	stream    string
	group     string
	consumer  string
	processor Processor
}

// NewConsumer creates one Redis consumer identity inside the configured group.
// Multiple processes can use the same group to horizontally scale delivery.
func NewConsumer(client *redis.Client, streamName string, group string, consumer string, processor Processor) *Consumer {
	if streamName == "" {
		streamName = DefaultStream
	}
	if group == "" {
		group = DefaultGroup
	}
	if consumer == "" {
		consumer = "worker"
	}
	return &Consumer{client: client, stream: streamName, group: group, consumer: consumer, processor: processor}
}

// EnsureGroup creates the consumer group if this is the first worker instance.
func (c *Consumer) EnsureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.stream, c.group, "0").Err()
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
		return err
	}
	return nil
}

// ReadAndProcess handles newly appended messages. It keeps processing the batch
// after individual failures so one bad notification does not starve later work.
func (c *Consumer) ReadAndProcess(ctx context.Context, count int64, block time.Duration) error {
	streams, err := c.client.XReadGroup(ctx, &redis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumer,
		Streams:  []string{c.stream, ">"},
		Count:    count,
		Block:    block,
	}).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}
	var firstErr error
	for _, stream := range streams {
		for _, msg := range stream.Messages {
			if err := c.processMessage(ctx, msg); err != nil {
				firstErr = errors.Join(firstErr, err)
			}
		}
	}
	return firstErr
}

// ClaimAndProcess reclaims pending messages that were delivered to another
// worker but not acked. The scan starts at 0-0 and advances with Redis' returned
// cursor until Redis signals completion by returning 0-0 again.
func (c *Consumer) ClaimAndProcess(ctx context.Context, minIdle time.Duration, count int64) error {
	start := "0-0"
	var firstErr error
	for {
		messages, next, err := c.client.XAutoClaim(ctx, &redis.XAutoClaimArgs{
			Stream:   c.stream,
			Group:    c.group,
			Consumer: c.consumer,
			MinIdle:  minIdle,
			Start:    start,
			Count:    count,
		}).Result()
		if err == redis.Nil {
			return firstErr
		}
		if err != nil {
			return errors.Join(firstErr, err)
		}
		for _, msg := range messages {
			if err := c.processMessage(ctx, msg); err != nil {
				firstErr = errors.Join(firstErr, err)
			}
		}
		if next == "0-0" {
			return firstErr
		}
		start = next
	}
}

// processMessage acks only after the durable processor has recorded the result.
// Malformed messages are acked to avoid a permanent poison entry in the group.
func (c *Consumer) processMessage(ctx context.Context, msg redis.XMessage) error {
	parsed, err := ParseDeliveryMessage(msg.Values)
	if err != nil {
		if _, ackErr := c.client.XAck(ctx, c.stream, c.group, msg.ID).Result(); ackErr != nil {
			return errors.Join(err, ackErr)
		}
		return nil
	}
	if err := c.processor.Process(ctx, parsed.NotificationID); err != nil {
		return err
	}
	_, err = c.client.XAck(ctx, c.stream, c.group, msg.ID).Result()
	return err
}
