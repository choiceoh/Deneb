package httpretry

import (
	"testing"
)

func TestAPIError_Error(t *testing.T) {
	err := &APIError{StatusCode: 429, Message: "rate limited"}
	want := "API error 429: rate limited"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestAPIError_IsRetryable(t *testing.T) {
	tests := []struct {
		code int
		want bool
	}{
		{400, false},
		{401, false},
		{429, true},
		{500, true},
		{503, true},
	}
	for _, tt := range tests {
		err := &APIError{StatusCode: tt.code}
		if got := err.IsRetryable(); got != tt.want {
			t.Errorf("APIError{%d}.IsRetryable() = %v, want %v", tt.code, got, tt.want)
		}
	}
}

func TestAPIError_IsRateLimited(t *testing.T) {
	if (&APIError{StatusCode: 429}).IsRateLimited() != true {
		t.Error("expected 429 to be rate limited")
	}
	if (&APIError{StatusCode: 500}).IsRateLimited() != false {
		t.Error("expected 500 to not be rate limited")
	}
}

