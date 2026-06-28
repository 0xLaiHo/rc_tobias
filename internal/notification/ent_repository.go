package notification

import (
	"context"
	"encoding/json"
	"time"

	"github.com/0xLaiHo/rc_tobias/ent"
	entdeliveryattempt "github.com/0xLaiHo/rc_tobias/ent/deliveryattempt"
	entnotification "github.com/0xLaiHo/rc_tobias/ent/notification"
	entoutboxevent "github.com/0xLaiHo/rc_tobias/ent/outboxevent"
	"github.com/0xLaiHo/rc_tobias/internal/delivery"
	"github.com/0xLaiHo/rc_tobias/internal/outbox"
	"github.com/google/uuid"
)

type EntRepository struct {
	client *ent.Client
	now    func() time.Time
}

// NewEntRepository adapts Ent into the notification service, delivery store,
// and outbox scheduling contracts. PostgreSQL remains the source of truth; Redis
// messages carry only IDs.
func NewEntRepository(client *ent.Client) *EntRepository {
	return &EntRepository{client: client, now: time.Now}
}

// CreateNotification writes the notification and its first outbox event in one
// transaction. This is the outbox pattern: a successfully accepted API request
// cannot commit without also creating durable work for the relay.
func (r *EntRepository) CreateNotification(ctx context.Context, req CreateRequest) (Notification, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return Notification{}, err
	}
	defer rollbackUnlessCommitted(tx)

	created, err := tx.Notification.Create().
		SetURL(req.URL).
		SetMethod(req.Method).
		SetHeaders(req.Headers).
		SetBody([]byte(req.Body)).
		SetStatus(entnotification.StatusPending).
		SetMaxAttempts(req.MaxAttempts).
		Save(ctx)
	if err != nil {
		return Notification{}, err
	}

	if _, err := tx.OutboxEvent.Create().
		SetNotificationID(created.ID).
		SetEventType(outbox.EventTypeNotificationDelivery).
		SetStatus(entoutboxevent.StatusPending).
		SetScheduledAt(r.now()).
		Save(ctx); err != nil {
		return Notification{}, err
	}

	if err := tx.Commit(); err != nil {
		return Notification{}, err
	}
	return mapEntNotification(created), nil
}

func (r *EntRepository) GetNotification(ctx context.Context, id uuid.UUID) (Notification, error) {
	got, err := r.client.Notification.Get(ctx, id)
	if err != nil {
		return Notification{}, mapEntError(err)
	}
	return mapEntNotification(got), nil
}

func (r *EntRepository) ListDeliveryAttempts(ctx context.Context, id uuid.UUID) ([]DeliveryAttempt, error) {
	attempts, err := r.client.DeliveryAttempt.Query().
		Where(entdeliveryattempt.NotificationIDEQ(id)).
		Order(entdeliveryattempt.ByCreatedAt()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]DeliveryAttempt, 0, len(attempts))
	for _, attempt := range attempts {
		out = append(out, mapEntAttempt(attempt))
	}
	return out, nil
}

// RetryFailedNotification is the manual compensation path. The conditional
// update prevents two concurrent retry calls from creating duplicate outbox
// events for the same failed notification.
func (r *EntRepository) RetryFailedNotification(ctx context.Context, id uuid.UUID) (Notification, error) {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return Notification{}, err
	}
	defer rollbackUnlessCommitted(tx)

	current, err := tx.Notification.Get(ctx, id)
	if err != nil {
		return Notification{}, mapEntError(err)
	}
	if current.Status != entnotification.StatusFailed {
		return Notification{}, ErrConflict
	}
	affected, err := tx.Notification.Update().
		Where(entnotification.IDEQ(id), entnotification.StatusEQ(entnotification.StatusFailed)).
		SetStatus(entnotification.StatusPending).
		SetAttemptCount(0).
		ClearLastError().
		ClearNextAttemptAt().
		Save(ctx)
	if err != nil {
		return Notification{}, err
	}
	if affected != 1 {
		return Notification{}, ErrConflict
	}
	if _, err := tx.OutboxEvent.Create().
		SetNotificationID(id).
		SetEventType(outbox.EventTypeNotificationDelivery).
		SetStatus(entoutboxevent.StatusPending).
		SetScheduledAt(r.now()).
		Save(ctx); err != nil {
		return Notification{}, err
	}
	if err := tx.Commit(); err != nil {
		return Notification{}, err
	}
	updated, err := r.client.Notification.Get(ctx, id)
	if err != nil {
		return Notification{}, mapEntError(err)
	}
	return mapEntNotification(updated), nil
}

// LoadForDelivery reads the durable task payload immediately before delivery.
// Redis intentionally stores only IDs so retries always use the latest database
// state rather than stale message payloads.
func (r *EntRepository) LoadForDelivery(ctx context.Context, id uuid.UUID) (delivery.Task, error) {
	got, err := r.client.Notification.Get(ctx, id)
	if err != nil {
		return delivery.Task{}, mapEntError(err)
	}
	return delivery.Task{
		ID:          got.ID,
		URL:         got.URL,
		Method:      got.Method,
		Headers:     got.Headers,
		Body:        got.Body,
		Status:      delivery.Status(got.Status),
		Attempt:     got.AttemptCount,
		MaxAttempts: got.MaxAttempts,
	}, nil
}

// MarkDelivering is a compare-and-set transition. Only pending/retrying tasks
// may become delivering, which keeps duplicated Redis messages from replaying a
// notification that already reached a terminal state.
func (r *EntRepository) MarkDelivering(ctx context.Context, id uuid.UUID) error {
	affected, err := r.client.Notification.Update().
		Where(
			entnotification.IDEQ(id),
			entnotification.StatusIn(entnotification.StatusPending, entnotification.StatusRetrying),
		).
		SetStatus(entnotification.StatusDelivering).
		ClearNextAttemptAt().
		Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrConflict
	}
	return nil
}

// RecordSuccess persists the terminal success and the audit attempt in the same
// transaction. The attempt counter predicate ties the result to the in-flight
// attempt observed by the worker.
func (r *EntRepository) RecordSuccess(ctx context.Context, id uuid.UUID, attempt delivery.RecordedAttempt) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)

	affected, err := tx.Notification.Update().
		Where(
			entnotification.IDEQ(id),
			entnotification.StatusEQ(entnotification.StatusDelivering),
			entnotification.AttemptCountEQ(attempt.AttemptNumber-1),
		).
		SetStatus(entnotification.StatusSucceeded).
		SetAttemptCount(attempt.AttemptNumber).
		ClearLastError().
		ClearNextAttemptAt().
		Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrConflict
	}
	if err := createAttempt(ctx, tx, id, attempt, entdeliveryattempt.StatusSucceeded); err != nil {
		return err
	}
	return tx.Commit()
}

// RecordFailure either schedules the next outbox event or marks the notification
// terminal. Like RecordSuccess, it checks the in-flight attempt before changing
// state so old messages cannot overwrite newer outcomes.
func (r *EntRepository) RecordFailure(ctx context.Context, id uuid.UUID, failure delivery.RecordedFailure) error {
	tx, err := r.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer rollbackUnlessCommitted(tx)

	status := entdeliveryattempt.StatusFailed
	notificationStatus := entnotification.StatusFailed
	if !failure.Terminal && failure.NextAttemptAt != nil {
		status = entdeliveryattempt.StatusRetrying
		notificationStatus = entnotification.StatusRetrying
	}
	update := tx.Notification.Update().
		Where(
			entnotification.IDEQ(id),
			entnotification.StatusEQ(entnotification.StatusDelivering),
			entnotification.AttemptCountEQ(failure.AttemptNumber-1),
		).
		SetStatus(notificationStatus).
		SetAttemptCount(failure.AttemptNumber).
		SetLastError(failure.ErrorMessage)
	if failure.NextAttemptAt != nil {
		update.SetNextAttemptAt(*failure.NextAttemptAt)
	} else {
		update.ClearNextAttemptAt()
	}
	affected, err := update.Save(ctx)
	if err != nil {
		return err
	}
	if affected != 1 {
		return ErrConflict
	}
	if err := createAttempt(ctx, tx, id, failure.RecordedAttempt, status); err != nil {
		return err
	}

	if !failure.Terminal && failure.NextAttemptAt != nil {
		if _, err := tx.OutboxEvent.Create().
			SetNotificationID(id).
			SetEventType(outbox.EventTypeNotificationDelivery).
			SetStatus(entoutboxevent.StatusPending).
			SetScheduledAt(*failure.NextAttemptAt).
			Save(ctx); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func createAttempt(ctx context.Context, tx *ent.Tx, id uuid.UUID, attempt delivery.RecordedAttempt, status entdeliveryattempt.Status) error {
	create := tx.DeliveryAttempt.Create().
		SetNotificationID(id).
		SetAttemptNumber(attempt.AttemptNumber).
		SetStatus(status).
		SetDurationMs(attempt.Duration.Milliseconds())
	if attempt.StatusCode > 0 {
		create.SetHTTPStatus(attempt.StatusCode)
	}
	if attempt.ErrorMessage != "" {
		create.SetErrorMessage(attempt.ErrorMessage)
	}
	_, err := create.Save(ctx)
	return err
}

func mapEntNotification(got *ent.Notification) Notification {
	if got.Headers == nil {
		got.Headers = map[string]string{}
	}
	return Notification{
		ID:            got.ID,
		URL:           got.URL,
		Method:        got.Method,
		Headers:       got.Headers,
		Body:          json.RawMessage(got.Body),
		Status:        Status(got.Status),
		AttemptCount:  got.AttemptCount,
		MaxAttempts:   got.MaxAttempts,
		LastError:     got.LastError,
		NextAttemptAt: got.NextAttemptAt,
		CreatedAt:     got.CreatedAt,
		UpdatedAt:     got.UpdatedAt,
	}
}

func mapEntAttempt(got *ent.DeliveryAttempt) DeliveryAttempt {
	return DeliveryAttempt{
		ID:             got.ID,
		NotificationID: got.NotificationID,
		AttemptNumber:  got.AttemptNumber,
		Status:         string(got.Status),
		HTTPStatus:     got.HTTPStatus,
		ErrorMessage:   got.ErrorMessage,
		DurationMS:     got.DurationMs,
		CreatedAt:      got.CreatedAt,
	}
}

func mapEntError(err error) error {
	if ent.IsNotFound(err) {
		return ErrNotFound
	}
	return err
}

func rollbackUnlessCommitted(tx *ent.Tx) {
	_ = tx.Rollback()
}
