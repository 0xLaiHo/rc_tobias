package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Status mirrors notification.Status without importing the notification package,
// keeping delivery logic independent from API/service types.
type Status string

const (
	StatusPending    Status = "pending"
	StatusDelivering Status = "delivering"
	StatusSucceeded  Status = "succeeded"
	StatusRetrying   Status = "retrying"
	StatusFailed     Status = "failed"
)

// Task is the immutable snapshot a worker needs for one HTTP attempt.
type Task struct {
	ID          uuid.UUID
	URL         string
	Method      string
	Headers     map[string]string
	Body        []byte
	Status      Status
	Attempt     int
	MaxAttempts int
}

// AttemptResult captures transport-level outcome only; supplier response bodies
// are intentionally not part of the delivery contract.
type AttemptResult struct {
	StatusCode int
	Err        error
	Duration   time.Duration
}

// RecordedAttempt is the audit payload persisted after any finished HTTP call.
type RecordedAttempt struct {
	AttemptNumber int
	StatusCode    int
	ErrorMessage  string
	Duration      time.Duration
}

// RecordedFailure adds retry scheduling information to a failed attempt.
type RecordedFailure struct {
	RecordedAttempt
	Terminal      bool
	NextAttemptAt *time.Time
}

// Store is the persistence boundary used by Processor. Implementations must
// enforce state compare-and-set semantics because Redis delivery is at-least-once.
type Store interface {
	LoadForDelivery(ctx context.Context, id uuid.UUID) (Task, error)
	MarkDelivering(ctx context.Context, id uuid.UUID) error
	RecordSuccess(ctx context.Context, id uuid.UUID, attempt RecordedAttempt) error
	RecordFailure(ctx context.Context, id uuid.UUID, failure RecordedFailure) error
}

// Dispatcher performs the external side effect for one delivery attempt.
type Dispatcher interface {
	Deliver(ctx context.Context, task Task) AttemptResult
}
