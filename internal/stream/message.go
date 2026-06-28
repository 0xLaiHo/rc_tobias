package stream

import (
	"fmt"

	"github.com/0xLaiHo/rc_tobias/internal/outbox"
	"github.com/google/uuid"
)

const (
	// DefaultStream and DefaultGroup are shared by relay and worker so local
	// deployments do not need additional queue configuration.
	DefaultStream = "notification-deliveries"
	DefaultGroup  = "notification-workers"

	fieldOutboxEventID  = "outbox_event_id"
	fieldNotificationID = "notification_id"
	fieldEventType      = "event_type"
)

// DeliveryMessage is deliberately small: Redis carries routing metadata, while
// PostgreSQL carries the full notification payload and state.
type DeliveryMessage struct {
	OutboxEventID  uuid.UUID
	NotificationID uuid.UUID
	EventType      string
}

// BuildDeliveryMessage converts a durable outbox event into Redis fields.
func BuildDeliveryMessage(event outbox.Event) map[string]any {
	return map[string]any{
		fieldOutboxEventID:  event.ID.String(),
		fieldNotificationID: event.NotificationID.String(),
		fieldEventType:      event.Type,
	}
}

// ParseDeliveryMessage validates Redis fields before the worker touches durable
// notification state.
func ParseDeliveryMessage(values map[string]any) (DeliveryMessage, error) {
	outboxID, err := parseUUIDField(values, fieldOutboxEventID)
	if err != nil {
		return DeliveryMessage{}, err
	}
	notificationID, err := parseUUIDField(values, fieldNotificationID)
	if err != nil {
		return DeliveryMessage{}, err
	}
	eventType, _ := values[fieldEventType].(string)
	if eventType == "" {
		eventType = outbox.EventTypeNotificationDelivery
	}
	return DeliveryMessage{
		OutboxEventID:  outboxID,
		NotificationID: notificationID,
		EventType:      eventType,
	}, nil
}

func parseUUIDField(values map[string]any, field string) (uuid.UUID, error) {
	raw, ok := values[field]
	if !ok {
		return uuid.Nil, fmt.Errorf("stream message missing %s", field)
	}
	text, ok := raw.(string)
	if !ok || text == "" {
		return uuid.Nil, fmt.Errorf("stream message field %s is not a string", field)
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return uuid.Nil, fmt.Errorf("stream message field %s is not a uuid: %w", field, err)
	}
	return id, nil
}
