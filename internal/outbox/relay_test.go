package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRelayPublishesClaimedEventsAndMarksPublished(t *testing.T) {
	eventID := uuid.New()
	notificationID := uuid.New()
	store := &fakeStore{events: []Event{{
		ID:             eventID,
		NotificationID: notificationID,
		ScheduledAt:    time.Unix(1000, 0),
	}}}
	publisher := &fakePublisher{streamID: "1700000000000-0"}
	relay := NewRelay(store, publisher, RelayConfig{BatchSize: 10, LockerID: "relay-a"})

	if err := relay.PublishDue(context.Background()); err != nil {
		t.Fatalf("PublishDue returned error: %v", err)
	}

	if len(publisher.published) != 1 {
		t.Fatalf("published events = %d, want 1", len(publisher.published))
	}
	if store.published[eventID] != "1700000000000-0:relay-a" {
		t.Fatalf("published stream id = %q, want 1700000000000-0", store.published[eventID])
	}
}

func TestRelayMarksEventFailedWhenPublishFails(t *testing.T) {
	eventID := uuid.New()
	notificationID := uuid.New()
	store := &fakeStore{events: []Event{{ID: eventID, NotificationID: notificationID}}}
	publisher := &fakePublisher{err: errors.New("redis unavailable")}
	relay := NewRelay(store, publisher, RelayConfig{BatchSize: 10, LockerID: "relay-a"})

	if err := relay.PublishDue(context.Background()); err != nil {
		t.Fatalf("PublishDue returned error: %v", err)
	}

	if store.failed[eventID] == "" {
		t.Fatal("failed event was not marked")
	}
}

type fakeStore struct {
	events    []Event
	published map[uuid.UUID]string
	failed    map[uuid.UUID]string
}

func (s *fakeStore) ClaimDue(ctx context.Context, limit int, lockerID string) ([]Event, error) {
	return s.events, nil
}

func (s *fakeStore) MarkPublished(ctx context.Context, id uuid.UUID, streamID string, lockerID string) error {
	if s.published == nil {
		s.published = make(map[uuid.UUID]string)
	}
	s.published[id] = streamID + ":" + lockerID
	return nil
}

func (s *fakeStore) MarkFailed(ctx context.Context, id uuid.UUID, reason string, lockerID string) error {
	if s.failed == nil {
		s.failed = make(map[uuid.UUID]string)
	}
	s.failed[id] = reason
	return nil
}

type fakePublisher struct {
	streamID  string
	err       error
	published []Event
}

func (p *fakePublisher) Publish(ctx context.Context, event Event) (string, error) {
	p.published = append(p.published, event)
	return p.streamID, p.err
}
