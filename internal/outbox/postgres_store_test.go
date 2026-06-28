package outbox

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func TestPostgresStoreMarkPublishedReturnsConflictWhenClaimLost(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	store := NewPostgresStore(db)
	id := uuid.New()

	mock.ExpectExec("UPDATE outbox_events").
		WithArgs(id, "stream-1", sqlmock.AnyArg(), "relay-a").
		WillReturnResult(sqlmock.NewResult(0, 0))

	err = store.MarkPublished(context.Background(), id, "stream-1", "relay-a")
	if !errors.Is(err, ErrLostClaim) {
		t.Fatalf("MarkPublished error = %v, want ErrLostClaim", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet sql expectations: %v", err)
	}
}
