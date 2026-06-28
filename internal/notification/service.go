package notification

import (
	"context"

	"github.com/google/uuid"
)

type Repository interface {
	CreateNotification(ctx context.Context, req CreateRequest) (Notification, error)
	GetNotification(ctx context.Context, id uuid.UUID) (Notification, error)
	ListDeliveryAttempts(ctx context.Context, id uuid.UUID) ([]DeliveryAttempt, error)
	RetryFailedNotification(ctx context.Context, id uuid.UUID) (Notification, error)
}

// Service owns request-level business validation and hides the persistence
// implementation from the HTTP layer.
type Service struct {
	repo Repository
}

// NewService constructs the application service around a repository.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// Create validates again below the HTTP boundary so non-HTTP callers cannot
// bypass normalization and target safety checks.
func (s *Service) Create(ctx context.Context, req CreateRequest) (Notification, error) {
	normalized, err := ValidateCreateRequest(req)
	if err != nil {
		return Notification{}, err
	}
	return s.repo.CreateNotification(ctx, normalized)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (Notification, error) {
	return s.repo.GetNotification(ctx, id)
}

func (s *Service) ListAttempts(ctx context.Context, id uuid.UUID) ([]DeliveryAttempt, error) {
	return s.repo.ListDeliveryAttempts(ctx, id)
}

func (s *Service) RetryFailed(ctx context.Context, id uuid.UUID) (Notification, error) {
	return s.repo.RetryFailedNotification(ctx, id)
}
