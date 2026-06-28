package delivery

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/0xLaiHo/rc_tobias/internal/netguard"
)

type HTTPDispatcher struct {
	client *http.Client
}

// NewHTTPDispatcher builds the outbound client used by workers. It disables
// redirect following so the retry policy sees the supplier's original response,
// and it resolves the dial address through netguard to reduce DNS rebinding
// risk after a notification has already been accepted.
func NewHTTPDispatcher(timeout time.Duration) *HTTPDispatcher {
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	dialer := &net.Dialer{Timeout: timeout}
	transport.DialContext = func(ctx context.Context, network string, address string) (net.Conn, error) {
		resolved, err := netguard.ResolvePublicAddress(ctx, address)
		if err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, resolved)
	}
	return &HTTPDispatcher{client: &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// Deliver performs one HTTP attempt and returns only transport/status metadata.
// The notification system deliberately ignores supplier response bodies because
// business-level interpretation is outside this service boundary.
func (d *HTTPDispatcher) Deliver(ctx context.Context, task Task) AttemptResult {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, task.Method, task.URL, bytes.NewReader(task.Body))
	if err != nil {
		return AttemptResult{Err: err, Duration: time.Since(start)}
	}
	for key, value := range task.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("X-Notification-ID", task.ID.String())
	req.Header.Set("Idempotency-Key", task.ID.String())

	resp, err := d.client.Do(req)
	if err != nil {
		return AttemptResult{Err: err, Duration: time.Since(start)}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return AttemptResult{StatusCode: resp.StatusCode, Duration: time.Since(start)}
}
