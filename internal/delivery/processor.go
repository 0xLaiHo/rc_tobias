package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type Processor struct {
	store      Store
	dispatcher Dispatcher
	policy     RetryPolicy
	now        func() time.Time
}

// NewProcessor wires the durable state store, HTTP dispatcher, and retry policy
// together. The injected clock keeps retry scheduling deterministic in tests.
func NewProcessor(store Store, dispatcher Dispatcher, policy RetryPolicy, now func() time.Time) *Processor {
	if now == nil {
		now = time.Now
	}
	return &Processor{store: store, dispatcher: dispatcher, policy: policy, now: now}
}

// Process handles a single at-least-once delivery message. State changes are
// compare-and-set inside the store, so duplicate Redis deliveries should either
// become no-ops or conflicts instead of overwriting terminal outcomes.
func (p *Processor) Process(ctx context.Context, id uuid.UUID) error {
	task, err := p.store.LoadForDelivery(ctx, id)
	if err != nil {
		return err
	}
	if task.Status == StatusSucceeded || task.Status == StatusFailed {
		return nil
	}
	if task.MaxAttempts == 0 {
		task.MaxAttempts = 5
	}

	if err := p.store.MarkDelivering(ctx, id); err != nil {
		return err
	}

	attemptNumber := task.Attempt + 1
	result := p.dispatcher.Deliver(ctx, task)
	recorded := RecordedAttempt{
		AttemptNumber: attemptNumber,
		StatusCode:    result.StatusCode,
		ErrorMessage:  errorMessage(result),
		Duration:      result.Duration,
	}

	if result.Err == nil && result.StatusCode >= 200 && result.StatusCode <= 299 {
		// Persist the result even if the caller context was canceled while the
		// external API was running; otherwise a delivered notification could lose
		// its audit record and retry decision.
		recordCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return p.store.RecordSuccess(recordCtx, id, recorded)
	}

	decision := p.policy.Decide(result, attemptNumber, task.MaxAttempts)
	failure := RecordedFailure{
		RecordedAttempt: recorded,
		Terminal:        decision.Terminal,
	}
	if decision.Retry {
		next := p.now().Add(decision.Delay)
		failure.NextAttemptAt = &next
	}
	// Use an internal context for the same reason as the success path: recording
	// the delivery outcome is part of reliability, not part of the caller's wait.
	recordCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return p.store.RecordFailure(recordCtx, id, failure)
}

func errorMessage(result AttemptResult) string {
	if result.Err != nil {
		return result.Err.Error()
	}
	if result.StatusCode == 0 {
		return "no HTTP response"
	}
	if result.StatusCode >= 200 && result.StatusCode <= 299 {
		return ""
	}
	return fmt.Sprintf("unexpected HTTP status %d", result.StatusCode)
}
