package notification

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

// Status is the public state machine exposed by the API and stored in
// PostgreSQL.
type Status string

const (
	StatusPending    Status = "pending"
	StatusDelivering Status = "delivering"
	StatusSucceeded  Status = "succeeded"
	StatusRetrying   Status = "retrying"
	StatusFailed     Status = "failed"
)

var (
	// ErrInvalid means the submitted notification cannot safely become durable
	// work.
	ErrInvalid  = errors.New("invalid notification request")
	ErrNotFound = errors.New("notification not found")
	// ErrConflict means a compare-and-set state transition lost a race or the
	// requested operation is not valid for the current state.
	ErrConflict = errors.New("notification state conflict")
)

// CreateRequest is the producer-facing payload. Header and body shapes are kept
// generic because supplier-specific templates are out of scope for the MVP.
type CreateRequest struct {
	URL         string            `json:"url"`
	Method      string            `json:"method,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Body        json.RawMessage   `json:"body,omitempty"`
	MaxAttempts int               `json:"max_attempts,omitempty"`
}

// Notification is the API view of the durable task record.
type Notification struct {
	ID            uuid.UUID         `json:"id"`
	URL           string            `json:"url"`
	Method        string            `json:"method"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          json.RawMessage   `json:"body,omitempty"`
	Status        Status            `json:"status"`
	AttemptCount  int               `json:"attempt_count"`
	MaxAttempts   int               `json:"max_attempts"`
	LastError     string            `json:"last_error,omitempty"`
	NextAttemptAt *time.Time        `json:"next_attempt_at,omitempty"`
	CreatedAt     time.Time         `json:"created_at"`
	UpdatedAt     time.Time         `json:"updated_at"`
}

// DeliveryAttempt is the API view of one outbound HTTP attempt.
type DeliveryAttempt struct {
	ID             uuid.UUID `json:"id"`
	NotificationID uuid.UUID `json:"notification_id"`
	AttemptNumber  int       `json:"attempt_number"`
	Status         string    `json:"status"`
	HTTPStatus     int       `json:"http_status,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	DurationMS     int64     `json:"duration_ms"`
	CreatedAt      time.Time `json:"created_at"`
}

type NotificationResponse = Notification
