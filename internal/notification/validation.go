package notification

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/0xLaiHo/rc_tobias/internal/netguard"
)

const (
	defaultMethod      = "POST"
	defaultMaxAttempts = 5
	maxBodyBytes       = 256 * 1024
)

var allowedMethods = map[string]struct{}{
	"POST":  {},
	"PUT":   {},
	"PATCH": {},
}

// ValidateCreateRequest normalizes client input before it reaches persistence.
// Network safety is checked again in the worker dialer, but rejecting unsafe
// URLs here prevents bad jobs from becoming durable work in the first place.
func ValidateCreateRequest(req CreateRequest) (CreateRequest, error) {
	req.Method = strings.ToUpper(strings.TrimSpace(req.Method))
	if req.Method == "" {
		req.Method = defaultMethod
	}
	if _, ok := allowedMethods[req.Method]; !ok {
		return CreateRequest{}, fmt.Errorf("%w: unsupported method %q", ErrInvalid, req.Method)
	}

	parsed, err := url.ParseRequestURI(strings.TrimSpace(req.URL))
	if err != nil || parsed.Host == "" {
		return CreateRequest{}, fmt.Errorf("%w: url must be absolute", ErrInvalid)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return CreateRequest{}, fmt.Errorf("%w: url scheme must be http or https", ErrInvalid)
	}
	if err := netguard.RejectBlockedHost(parsed.Hostname()); err != nil {
		return CreateRequest{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	req.URL = parsed.String()

	if req.MaxAttempts == 0 {
		req.MaxAttempts = defaultMaxAttempts
	}
	if req.MaxAttempts < 1 || req.MaxAttempts > 20 {
		return CreateRequest{}, fmt.Errorf("%w: max_attempts must be between 1 and 20", ErrInvalid)
	}

	if len(req.Body) > maxBodyBytes {
		return CreateRequest{}, fmt.Errorf("%w: body exceeds %d bytes", ErrInvalid, maxBodyBytes)
	}
	if len(req.Body) > 0 && !json.Valid(req.Body) {
		return CreateRequest{}, fmt.Errorf("%w: body must be valid JSON", ErrInvalid)
	}
	if req.Headers == nil {
		req.Headers = map[string]string{}
	}
	for key, value := range req.Headers {
		if !validHeaderName(key) {
			return CreateRequest{}, fmt.Errorf("%w: invalid header name %q", ErrInvalid, key)
		}
		if strings.ContainsAny(value, "\r\n") {
			return CreateRequest{}, fmt.Errorf("%w: invalid header value for %q", ErrInvalid, key)
		}
	}

	return req, nil
}

func validHeaderName(name string) bool {
	if strings.TrimSpace(name) == "" {
		return false
	}
	for _, r := range name {
		if r <= 31 || r == 127 || strings.ContainsRune("()<>@,;:\\\"/[]?={}", r) || r == ' ' || r == '\t' {
			return false
		}
	}
	return true
}
