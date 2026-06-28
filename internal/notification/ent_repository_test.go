package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/0xLaiHo/rc_tobias/ent/enttest"
	entnotification "github.com/0xLaiHo/rc_tobias/ent/notification"
	"github.com/0xLaiHo/rc_tobias/internal/delivery"
	_ "github.com/mattn/go-sqlite3"
)

func TestEntRepositoryMarkDeliveringRejectsAlreadyDelivering(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:mark-delivering?mode=memory&_fk=1")
	defer client.Close()
	repo := NewEntRepository(client)

	created, err := client.Notification.Create().
		SetURL("https://vendor.example/hooks").
		SetMethod("POST").
		SetStatus(entnotification.StatusDelivering).
		SetMaxAttempts(5).
		Save(ctx)
	if err != nil {
		t.Fatalf("create notification: %v", err)
	}

	err = repo.MarkDelivering(ctx, created.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("MarkDelivering error = %v, want ErrConflict", err)
	}
}

func TestEntRepositoryRecordSuccessRejectsTerminalNotification(t *testing.T) {
	ctx := context.Background()
	client := enttest.Open(t, "sqlite3", "file:record-success?mode=memory&_fk=1")
	defer client.Close()
	repo := NewEntRepository(client)

	created, err := client.Notification.Create().
		SetURL("https://vendor.example/hooks").
		SetMethod("POST").
		SetStatus(entnotification.StatusSucceeded).
		SetAttemptCount(1).
		SetMaxAttempts(5).
		Save(ctx)
	if err != nil {
		t.Fatalf("create notification: %v", err)
	}

	err = repo.RecordSuccess(ctx, created.ID, delivery.RecordedAttempt{
		AttemptNumber: 1,
		StatusCode:    204,
		Duration:      time.Millisecond,
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("RecordSuccess error = %v, want ErrConflict", err)
	}

	attempts, err := repo.ListDeliveryAttempts(ctx, created.ID)
	if err != nil {
		t.Fatalf("list attempts: %v", err)
	}
	if len(attempts) != 0 {
		t.Fatalf("attempts = %d, want 0 for stale terminal update", len(attempts))
	}
}
