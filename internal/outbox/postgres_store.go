package outbox

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore implements outbox ownership with PostgreSQL row locks rather
// than an external distributed lock service.
func NewPostgresStore(db *sql.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

// ClaimDue atomically marks due rows as publishing and returns the claimed
// events. FOR UPDATE SKIP LOCKED lets multiple relay instances share the queue
// without blocking each other on rows already claimed by peers.
func (s *PostgresStore) ClaimDue(ctx context.Context, limit int, lockerID string) ([]Event, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
WITH picked AS (
	SELECT id
	FROM outbox_events
	WHERE scheduled_at <= NOW()
	  AND (
		status = 'pending'
		OR (status = 'publishing' AND locked_at < NOW() - INTERVAL '5 minutes')
	  )
	ORDER BY scheduled_at ASC, created_at ASC
	FOR UPDATE SKIP LOCKED
	LIMIT $1
)
UPDATE outbox_events AS oe
SET status = 'publishing',
	locked_by = $2,
	locked_at = NOW(),
	last_error = '',
	updated_at = NOW()
FROM picked
WHERE oe.id = picked.id
RETURNING oe.id, oe.notification_id, oe.event_type, oe.scheduled_at
`, limit, lockerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(&event.ID, &event.NotificationID, &event.Type, &event.ScheduledAt); err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

// MarkPublished finishes only the row still owned by this relay. The locker
// check prevents a stale relay from overwriting a row that was reclaimed.
func (s *PostgresStore) MarkPublished(ctx context.Context, id uuid.UUID, streamID string, lockerID string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE outbox_events
SET status = 'published',
	stream_id = $2,
	published_at = $3,
	locked_by = '',
	locked_at = NULL,
	updated_at = $3
WHERE id = $1
  AND status = 'publishing'
  AND locked_by = $4
`, id, streamID, time.Now(), lockerID)
	if err != nil {
		return err
	}
	return requireOneRow(result)
}

// MarkFailed releases the current claim after a publish failure so the event can
// be picked up again by a later relay pass.
func (s *PostgresStore) MarkFailed(ctx context.Context, id uuid.UUID, reason string, lockerID string) error {
	result, err := s.db.ExecContext(ctx, `
UPDATE outbox_events
SET status = 'pending',
	last_error = $2,
	locked_by = '',
	locked_at = NULL,
	updated_at = $3
WHERE id = $1
  AND status = 'publishing'
  AND locked_by = $4
`, id, reason, time.Now(), lockerID)
	if err != nil {
		return err
	}
	return requireOneRow(result)
}

func requireOneRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrLostClaim
	}
	return nil
}
