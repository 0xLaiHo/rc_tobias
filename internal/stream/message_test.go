package stream

import (
	"testing"

	"github.com/0xLaiHo/rc_tobias/internal/outbox"
	"github.com/google/uuid"
)

func TestBuildAndParseDeliveryMessage(t *testing.T) {
	event := outbox.Event{
		ID:             uuid.New(),
		NotificationID: uuid.New(),
		Type:           outbox.EventTypeNotificationDelivery,
	}

	values := BuildDeliveryMessage(event)
	parsed, err := ParseDeliveryMessage(values)
	if err != nil {
		t.Fatalf("ParseDeliveryMessage returned error: %v", err)
	}

	if parsed.OutboxEventID != event.ID {
		t.Fatalf("outbox id = %s, want %s", parsed.OutboxEventID, event.ID)
	}
	if parsed.NotificationID != event.NotificationID {
		t.Fatalf("notification id = %s, want %s", parsed.NotificationID, event.NotificationID)
	}
}

func TestParseDeliveryMessageRejectsMissingNotificationID(t *testing.T) {
	_, err := ParseDeliveryMessage(map[string]any{
		"outbox_event_id": uuid.NewString(),
	})
	if err == nil {
		t.Fatal("ParseDeliveryMessage returned nil error, want missing notification_id error")
	}
}
