package delivery

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestRetryPolicyRetriesNetworkErrors(t *testing.T) {
	policy := DefaultRetryPolicy()

	decision := policy.Decide(AttemptResult{
		Err: errors.New("connection reset"),
	}, 1, 5)

	if !decision.Retry {
		t.Fatal("Retry = false, want true")
	}
	if decision.Terminal {
		t.Fatal("Terminal = true, want false")
	}
	if decision.Delay <= 0 {
		t.Fatalf("Delay = %s, want positive delay", decision.Delay)
	}
}

func TestRetryPolicyRetriesRateLimitedResponses(t *testing.T) {
	policy := DefaultRetryPolicy()

	decision := policy.Decide(AttemptResult{StatusCode: 429}, 2, 5)

	if !decision.Retry {
		t.Fatal("Retry = false, want true")
	}
	if decision.Delay < 4*time.Second {
		t.Fatalf("Delay = %s, want exponential backoff of at least 4s", decision.Delay)
	}
}

func TestRetryPolicyTreatsMostClientErrorsAsTerminal(t *testing.T) {
	policy := DefaultRetryPolicy()

	decision := policy.Decide(AttemptResult{StatusCode: 400}, 1, 5)

	if decision.Retry {
		t.Fatal("Retry = true, want false")
	}
	if !decision.Terminal {
		t.Fatal("Terminal = false, want true")
	}
}

func TestRetryPolicyStopsAtMaxAttempts(t *testing.T) {
	policy := DefaultRetryPolicy()

	decision := policy.Decide(AttemptResult{StatusCode: 500}, 5, 5)

	if decision.Retry {
		t.Fatal("Retry = true, want false")
	}
	if !decision.Terminal {
		t.Fatal("Terminal = false, want true")
	}
}

func TestHTTPDispatcherDoesNotFollowRedirects(t *testing.T) {
	dispatcher := NewHTTPDispatcher(time.Second)

	err := dispatcher.client.CheckRedirect(&http.Request{}, []*http.Request{{}})
	if !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("CheckRedirect error = %v, want ErrUseLastResponse", err)
	}
}
