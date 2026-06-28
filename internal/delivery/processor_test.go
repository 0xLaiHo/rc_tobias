package delivery

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestProcessorMarksSuccessfulDelivery(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		task: Task{
			ID:          id,
			URL:         "https://vendor.example/hooks",
			Method:      "POST",
			Headers:     map[string]string{"X-Event": "registration"},
			Body:        []byte(`{"event":"registered"}`),
			Attempt:     0,
			MaxAttempts: 5,
		},
	}
	dispatcher := &fakeDispatcher{result: AttemptResult{StatusCode: 204, Duration: 10 * time.Millisecond}}
	processor := NewProcessor(store, dispatcher, DefaultRetryPolicy(), func() time.Time {
		return time.Unix(1000, 0)
	})

	if err := processor.Process(context.Background(), id); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if store.success == nil {
		t.Fatal("success result was not recorded")
	}
	if store.success.AttemptNumber != 1 {
		t.Fatalf("attempt number = %d, want 1", store.success.AttemptNumber)
	}
	if store.failure != nil {
		t.Fatalf("failure result recorded unexpectedly: %+v", store.failure)
	}
}

func TestProcessorSchedulesRetryForTransientFailure(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		task: Task{
			ID:          id,
			URL:         "https://vendor.example/hooks",
			Method:      "POST",
			Body:        []byte(`{"event":"registered"}`),
			Attempt:     1,
			MaxAttempts: 5,
		},
	}
	dispatcher := &fakeDispatcher{result: AttemptResult{StatusCode: 500, Duration: 20 * time.Millisecond}}
	processor := NewProcessor(store, dispatcher, DefaultRetryPolicy(), func() time.Time {
		return time.Unix(1000, 0)
	})

	if err := processor.Process(context.Background(), id); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	if store.failure == nil {
		t.Fatal("failure result was not recorded")
	}
	if store.failure.Terminal {
		t.Fatal("Terminal = true, want false")
	}
	if store.failure.NextAttemptAt == nil {
		t.Fatal("NextAttemptAt is nil, want retry schedule")
	}
	if !store.failure.NextAttemptAt.After(time.Unix(1000, 0)) {
		t.Fatalf("NextAttemptAt = %s, want after current time", store.failure.NextAttemptAt)
	}
}

func TestProcessorSkipsAlreadySucceededTasks(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		task: Task{ID: id, Status: StatusSucceeded, Attempt: 1, MaxAttempts: 5},
	}
	dispatcher := &fakeDispatcher{err: errors.New("dispatcher should not be called")}
	processor := NewProcessor(store, dispatcher, DefaultRetryPolicy(), time.Now)

	if err := processor.Process(context.Background(), id); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if dispatcher.called {
		t.Fatal("dispatcher was called for an already succeeded task")
	}
}

func TestProcessorRecordsResultAfterCallerContextCancellation(t *testing.T) {
	id := uuid.New()
	store := &fakeStore{
		task: Task{
			ID:          id,
			URL:         "https://vendor.example/hooks",
			Method:      "POST",
			Status:      StatusPending,
			MaxAttempts: 5,
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	dispatcher := &fakeDispatcher{
		result: AttemptResult{StatusCode: 204, Duration: 10 * time.Millisecond},
		onDeliver: func() {
			cancel()
		},
	}
	processor := NewProcessor(store, dispatcher, DefaultRetryPolicy(), time.Now)

	if err := processor.Process(ctx, id); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}
	if store.success == nil {
		t.Fatal("success result was not recorded")
	}
	if store.recordSuccessCtxErr != nil {
		t.Fatalf("RecordSuccess received canceled context: %v", store.recordSuccessCtxErr)
	}
}

type fakeStore struct {
	task                Task
	success             *RecordedAttempt
	failure             *RecordedFailure
	recordSuccessCtxErr error
}

func (s *fakeStore) LoadForDelivery(ctx context.Context, id uuid.UUID) (Task, error) {
	return s.task, nil
}

func (s *fakeStore) MarkDelivering(ctx context.Context, id uuid.UUID) error {
	return nil
}

func (s *fakeStore) RecordSuccess(ctx context.Context, id uuid.UUID, attempt RecordedAttempt) error {
	s.recordSuccessCtxErr = ctx.Err()
	s.success = &attempt
	return nil
}

func (s *fakeStore) RecordFailure(ctx context.Context, id uuid.UUID, failure RecordedFailure) error {
	s.failure = &failure
	return nil
}

type fakeDispatcher struct {
	result    AttemptResult
	err       error
	called    bool
	onDeliver func()
}

func (d *fakeDispatcher) Deliver(ctx context.Context, task Task) AttemptResult {
	d.called = true
	if d.onDeliver != nil {
		d.onDeliver()
	}
	if d.err != nil {
		return AttemptResult{Err: d.err}
	}
	return d.result
}
