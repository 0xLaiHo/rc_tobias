package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/0xLaiHo/rc_tobias/internal/notification"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestCreateNotificationReturnsAccepted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	id := uuid.New()
	service := &fakeNotificationService{
		created: notification.Notification{
			ID:          id,
			URL:         "https://vendor.example/hooks",
			Method:      "POST",
			Status:      notification.StatusPending,
			MaxAttempts: 5,
			CreatedAt:   time.Unix(1000, 0),
			UpdatedAt:   time.Unix(1000, 0),
		},
	}
	router := NewRouter(service)
	body := []byte(`{"url":"https://vendor.example/hooks","body":{"event":"registered"}}`)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/notifications", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	var got notification.NotificationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response JSON error: %v", err)
	}
	if got.ID != id {
		t.Fatalf("id = %s, want %s", got.ID, id)
	}
}

func TestCreateNotificationRejectsInvalidPayload(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeNotificationService{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/notifications", bytes.NewReader([]byte(`{"url":"ftp://bad","body":{}}`)))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGetNotificationReturnsNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := NewRouter(&fakeNotificationService{getErr: notification.ErrNotFound})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/notifications/"+uuid.NewString(), nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

type fakeNotificationService struct {
	created notification.Notification
	get     notification.Notification
	getErr  error
}

func (s *fakeNotificationService) Create(ctx context.Context, req notification.CreateRequest) (notification.Notification, error) {
	return s.created, nil
}

func (s *fakeNotificationService) Get(ctx context.Context, id uuid.UUID) (notification.Notification, error) {
	return s.get, s.getErr
}

func (s *fakeNotificationService) ListAttempts(ctx context.Context, id uuid.UUID) ([]notification.DeliveryAttempt, error) {
	return nil, nil
}

func (s *fakeNotificationService) RetryFailed(ctx context.Context, id uuid.UUID) (notification.Notification, error) {
	return s.get, nil
}
