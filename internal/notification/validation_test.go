package notification

import (
	"encoding/json"
	"testing"
)

func TestValidateCreateRequestDefaultsMethodAndMaxAttempts(t *testing.T) {
	req := CreateRequest{
		URL:  "https://vendor.example/hooks",
		Body: json.RawMessage(`{"event":"registered"}`),
	}

	normalized, err := ValidateCreateRequest(req)
	if err != nil {
		t.Fatalf("ValidateCreateRequest returned error: %v", err)
	}

	if normalized.Method != "POST" {
		t.Fatalf("method = %q, want POST", normalized.Method)
	}
	if normalized.MaxAttempts != 5 {
		t.Fatalf("max attempts = %d, want 5", normalized.MaxAttempts)
	}
}

func TestValidateCreateRequestRejectsUnsupportedURLSchemes(t *testing.T) {
	req := CreateRequest{
		URL:    "ftp://vendor.example/hooks",
		Method: "POST",
		Body:   json.RawMessage(`{"event":"registered"}`),
	}

	_, err := ValidateCreateRequest(req)
	if err == nil {
		t.Fatal("ValidateCreateRequest returned nil error, want invalid URL error")
	}
}

func TestValidateCreateRequestRejectsInternalTargets(t *testing.T) {
	tests := []string{
		"http://localhost/hooks",
		"http://127.0.0.1/hooks",
		"http://10.0.0.5/hooks",
		"http://169.254.169.254/latest/meta-data",
		"http://[::1]/hooks",
	}

	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			req := CreateRequest{
				URL:    target,
				Method: "POST",
				Body:   json.RawMessage(`{"event":"registered"}`),
			}

			_, err := ValidateCreateRequest(req)
			if err == nil {
				t.Fatal("ValidateCreateRequest returned nil error, want internal target error")
			}
		})
	}
}

func TestValidateCreateRequestRejectsUnsafeHeaders(t *testing.T) {
	req := CreateRequest{
		URL:    "https://vendor.example/hooks",
		Method: "POST",
		Headers: map[string]string{
			"Content\nType": "application/json",
		},
		Body: json.RawMessage(`{"event":"registered"}`),
	}

	_, err := ValidateCreateRequest(req)
	if err == nil {
		t.Fatal("ValidateCreateRequest returned nil error, want invalid header error")
	}
}

func TestValidateCreateRequestRejectsInvalidJSONBody(t *testing.T) {
	req := CreateRequest{
		URL:    "https://vendor.example/hooks",
		Method: "POST",
		Body:   json.RawMessage(`{"event":`),
	}

	_, err := ValidateCreateRequest(req)
	if err == nil {
		t.Fatal("ValidateCreateRequest returned nil error, want invalid JSON body error")
	}
}
