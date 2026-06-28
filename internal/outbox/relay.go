package outbox

import "context"

type Relay struct {
	store     Store
	publisher Publisher
	config    RelayConfig
}

// NewRelay creates the component that bridges durable PostgreSQL outbox rows to
// Redis Streams. Multiple relays may run concurrently because the store uses
// row-level claims.
func NewRelay(store Store, publisher Publisher, config RelayConfig) *Relay {
	if config.BatchSize <= 0 {
		config.BatchSize = 50
	}
	if config.LockerID == "" {
		config.LockerID = "relay"
	}
	return &Relay{store: store, publisher: publisher, config: config}
}

// PublishDue publishes currently due outbox rows. A Redis publish failure moves
// the row back to pending so a later relay cycle can retry without losing work.
func (r *Relay) PublishDue(ctx context.Context) error {
	events, err := r.store.ClaimDue(ctx, r.config.BatchSize, r.config.LockerID)
	if err != nil {
		return err
	}
	for _, event := range events {
		streamID, err := r.publisher.Publish(ctx, event)
		if err != nil {
			if markErr := r.store.MarkFailed(ctx, event.ID, err.Error(), r.config.LockerID); markErr != nil {
				return markErr
			}
			continue
		}
		if err := r.store.MarkPublished(ctx, event.ID, streamID, r.config.LockerID); err != nil {
			return err
		}
	}
	return nil
}
