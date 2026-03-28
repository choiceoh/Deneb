package discord

import (
	"testing"
	"time"
)

func TestRetryDelay_ExponentialBackoff(t *testing.T) {
	// Without API error: exponential backoff.
	d1 := retryDelay(1, nil)
	if d1 != 1*time.Second {
		t.Errorf("attempt 1: expected 1s, got %v", d1)
	}
	d2 := retryDelay(2, nil)
	if d2 != 2*time.Second {
		t.Errorf("attempt 2: expected 2s, got %v", d2)
	}
	d3 := retryDelay(3, nil)
	if d3 != 4*time.Second {
		t.Errorf("attempt 3: expected 4s, got %v", d3)
	}
}

func TestRetryDelay_Cap(t *testing.T) {
	d := retryDelay(10, nil) // 2^9 = 512s, should be capped
	if d > 15*time.Second {
		t.Errorf("expected cap at 15s, got %v", d)
	}
}

func TestRetryDelay_RetryAfter(t *testing.T) {
	err := &APIError{StatusCode: 429, RetryAfter: 5 * time.Second}
	d := retryDelay(1, err)
	if d != 5*time.Second {
		t.Errorf("expected Retry-After 5s, got %v", d)
	}
}

func TestAPIError_IsRateLimited(t *testing.T) {
	err := &APIError{StatusCode: 429}
	if !err.IsRateLimited() {
		t.Error("expected rate limited")
	}

	err2 := &APIError{StatusCode: 400}
	if err2.IsRateLimited() {
		t.Error("expected not rate limited")
	}
}

func TestIsDiscordAPIError(t *testing.T) {
	var target *APIError

	// Non-API error.
	if isDiscordAPIError(nil, &target) {
		t.Error("expected false for nil")
	}

	// API error.
	err := &APIError{StatusCode: 429}
	if !isDiscordAPIError(err, &target) {
		t.Error("expected true for APIError")
	}
	if target.StatusCode != 429 {
		t.Errorf("expected 429, got %d", target.StatusCode)
	}
}
