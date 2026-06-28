package outbox

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

const EventTypeNotificationDelivery = "notification.delivery_requested"

// ErrLostClaim means a relay tried to finish an outbox event that another relay
// has already reclaimed or completed. Treating it as an error makes ownership
// races visible instead of silently corrupting event state.
var ErrLostClaim = errors.New("outbox event claim was lost")

type Event struct {
	ID             uuid.UUID
	NotificationID uuid.UUID
	Type           string
	ScheduledAt    time.Time
}

type Store interface {
	ClaimDue(ctx context.Context, limit int, lockerID string) ([]Event, error)
	MarkPublished(ctx context.Context, id uuid.UUID, streamID string, lockerID string) error
	MarkFailed(ctx context.Context, id uuid.UUID, reason string, lockerID string) error
}

type Publisher interface {
	Publish(ctx context.Context, event Event) (string, error)
}

type RelayConfig struct {
	BatchSize int
	LockerID  string
}
