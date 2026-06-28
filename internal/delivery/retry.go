package delivery

import "time"

type RetryDecision struct {
	Retry    bool
	Terminal bool
	Delay    time.Duration
}

type RetryPolicy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

// DefaultRetryPolicy keeps retry behavior intentionally simple for the MVP:
// exponential backoff with a cap and no supplier-specific policy table.
func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{
		BaseDelay: 2 * time.Second,
		MaxDelay:  5 * time.Minute,
	}
}

// Decide maps one HTTP attempt to the next state transition. Most 4xx responses
// are terminal because resending the same malformed request is unlikely to help.
func (p RetryPolicy) Decide(result AttemptResult, attemptNumber int, maxAttempts int) RetryDecision {
	if attemptNumber >= maxAttempts {
		return RetryDecision{Terminal: true}
	}
	if result.Err != nil || isRetryableStatus(result.StatusCode) {
		return RetryDecision{Retry: true, Delay: p.delay(attemptNumber)}
	}
	if result.StatusCode >= 200 && result.StatusCode <= 299 {
		return RetryDecision{}
	}
	return RetryDecision{Terminal: true}
}

func (p RetryPolicy) delay(attemptNumber int) time.Duration {
	if p.BaseDelay <= 0 {
		p.BaseDelay = 2 * time.Second
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 5 * time.Minute
	}
	delay := p.BaseDelay
	for i := 1; i < attemptNumber; i++ {
		delay *= 2
		if delay >= p.MaxDelay {
			return p.MaxDelay
		}
	}
	return delay
}

func isRetryableStatus(status int) bool {
	if status == 0 {
		return false
	}
	if status == 408 || status == 409 || status == 425 || status == 429 {
		return true
	}
	return status >= 500 && status <= 599
}
