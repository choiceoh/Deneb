package httpretry

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		status int
		want   Category
	}{
		// Rate limit.
		{429, CategoryRateLimit},

		// Timeouts.
		{408, CategoryTimeout},
		{504, CategoryTimeout},

		// Transient server errors.
		{500, CategoryTransient},
		{502, CategoryTransient},
		{503, CategoryTransient},
		{529, CategoryTransient},

		// Permanent client errors — must never retry.
		{400, CategoryNone},
		{401, CategoryNone},
		{403, CategoryNone},
		{404, CategoryNone},
		{405, CategoryNone},
		{410, CategoryNone}, // Gone: resource permanently removed.
		{422, CategoryNone},

		// Permanent server errors — must never retry.
		{501, CategoryNone}, // Not Implemented: retrying will never succeed.

		// Success codes — not retryable.
		{200, CategoryNone},
		{201, CategoryNone},
		{204, CategoryNone},
	}

	for _, tt := range tests {
		if got := Classify(tt.status); got != tt.want {
			t.Errorf("Classify(%d) = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestIsRetryable(t *testing.T) {
	retryable := []int{408, 429, 500, 502, 503, 504, 529}
	for _, s := range retryable {
		if !IsRetryable(s) {
			t.Errorf("IsRetryable(%d) = false, want true", s)
		}
	}

	nonRetryable := []int{200, 400, 401, 403, 404, 405, 410, 422, 501}
	for _, s := range nonRetryable {
		if IsRetryable(s) {
			t.Errorf("IsRetryable(%d) = true, want false", s)
		}
	}
}
