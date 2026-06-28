package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/0xLaiHo/rc_tobias/internal/notification"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type notificationService interface {
	Create(ctx context.Context, req notification.CreateRequest) (notification.Notification, error)
	Get(ctx context.Context, id uuid.UUID) (notification.Notification, error)
	ListAttempts(ctx context.Context, id uuid.UUID) ([]notification.DeliveryAttempt, error)
	RetryFailed(ctx context.Context, id uuid.UUID) (notification.Notification, error)
}

// ReadinessFunc is injected by cmd/api so routing stays testable while /readyz
// still checks real PostgreSQL and Redis dependencies in production.
type ReadinessFunc func(ctx context.Context) error

// NewRouter builds the test-friendly router without dependency readiness checks.
func NewRouter(service notificationService) http.Handler {
	return NewRouterWithReadiness(service, nil)
}

// NewRouterWithReadiness exposes the public API surface for producers and
// operators. The handlers keep validation at the HTTP boundary before delegating
// to the service layer.
func NewRouterWithReadiness(service notificationService, ready ReadinessFunc) http.Handler {
	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.GET("/readyz", func(c *gin.Context) {
		if ready != nil {
			if err := ready(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not ready"})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	router.POST("/notifications", createNotification(service))
	router.GET("/notifications/:id", getNotification(service))
	router.GET("/notifications/:id/attempts", listAttempts(service))
	router.POST("/notifications/:id/retry", retryNotification(service))

	return router
}

func createNotification(service notificationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req notification.CreateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON payload"})
			return
		}
		// Validate before calling the service so invalid requests are rejected at
		// the API boundary even when handlers are tested with a fake service.
		normalized, err := notification.ValidateCreateRequest(req)
		if err != nil {
			writeError(c, err)
			return
		}
		created, err := service.Create(c.Request.Context(), normalized)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, created)
	}
}

func getNotification(service notificationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		got, err := service.Get(c.Request.Context(), id)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusOK, got)
	}
}

func listAttempts(service notificationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		attempts, err := service.ListAttempts(c.Request.Context(), id)
		if err != nil {
			writeError(c, err)
			return
		}
		if attempts == nil {
			attempts = []notification.DeliveryAttempt{}
		}
		c.JSON(http.StatusOK, gin.H{"attempts": attempts})
	}
}

func retryNotification(service notificationService) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := parseID(c)
		if !ok {
			return
		}
		got, err := service.RetryFailed(c.Request.Context(), id)
		if err != nil {
			writeError(c, err)
			return
		}
		c.JSON(http.StatusAccepted, got)
	}
}

func parseID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid notification id"})
		return uuid.Nil, false
	}
	return id, true
}

func writeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, notification.ErrInvalid):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case errors.Is(err, notification.ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
	case errors.Is(err, notification.ErrConflict):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
	}
}
