package autoreply

import "testing"

// TestClassifyAgentError_LegacyMapping guards the migration of the local
// classifier internals to pkg/llmerr. The cases here cover each legacy
// branch of the pre-migration substring classifier so a regression in
// llmerr's pattern tables would surface here rather than silently
// reclassifying an autoreply-flow error.
func TestClassifyAgentError_LegacyMapping(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want AgentErrorKind
	}{
		// Role-ordering and compaction are local-only (no llmerr equivalent).
		{"role ordering (alternate)", "roles must alternate between user and assistant", AgentErrorRoleOrdering},
		{"role ordering (incorrect)", "incorrect role sequence", AgentErrorRoleOrdering},
		{"compaction fail", "compaction failed mid-run", AgentErrorCompaction},
		{"compaction error", "compaction error: exceeded retries", AgentErrorCompaction},

		// Context overflow — legacy loose phrases + explicit llmerr patterns.
		{"context overflow", "context overflow encountered", AgentErrorContextOverflow},
		{"context too large", "context too large for model", AgentErrorContextOverflow},
		{"context exceeded", "context exceeded limit", AgentErrorContextOverflow},
		{"context too long", "context too long for model", AgentErrorContextOverflow},
		{"max_tokens", "max_tokens reached", AgentErrorContextOverflow},
		{"token limit", "token limit reached", AgentErrorContextOverflow},

		// Auth — ensure 401 / invalid-key paths still map to Auth.
		{"401 status", "HTTP 401: unauthorized", AgentErrorAuth},
		{"unauthorized word", "unauthorized access", AgentErrorAuth},
		{"invalid api key", "invalid_api_key provided", AgentErrorAuth},

		// Billing.
		{"billing word", "billing not active on account", AgentErrorBilling},
		{"insufficient_quota", "insufficient_quota: you exceeded your current quota", AgentErrorBilling},

		// Rate limit (429).
		{"429", "HTTP 429 too many requests", AgentErrorRateLimit},
		{"rate limit message", "rate limit exceeded for requests per minute", AgentErrorRateLimit},

		// Server down — 5xx bucket.
		{"502", "HTTP 502 bad gateway", AgentErrorServerDown},
		{"503", "HTTP 503 service unavailable", AgentErrorServerDown},
		{"529", "HTTP 529 overloaded", AgentErrorServerDown},

		// Unknown fallback.
		{"gibberish", "completely unrelated error", AgentErrorUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ClassifyAgentError(tc.msg)
			if got != tc.want {
				t.Fatalf("ClassifyAgentError(%q) = %v, want %v", tc.msg, got, tc.want)
			}
		})
	}
}

// TestIsTransientHTTPError_Matrix pins IsTransient behaviour against the
// llmerr-backed classifier. Only rate-limit and 5xx server-down errors are
// transient; auth, billing, context overflow, and unknown are terminal.
func TestIsTransientHTTPError_Matrix(t *testing.T) {
	cases := map[string]bool{
		"HTTP 429 too many requests":   true,
		"rate limit exceeded":          true,
		"HTTP 502 bad gateway":         true,
		"HTTP 503 service unavailable": true,
		"HTTP 529 overloaded":          true,
		"HTTP 401: unauthorized":       false,
		"billing not active":           false,
		"context overflow encountered": false,
		"roles must alternate":         false,
		"compaction failed":            false,
		"completely unrelated error":   false,
	}
	for msg, want := range cases {
		t.Run(msg, func(t *testing.T) {
			if got := IsTransientHTTPError(msg); got != want {
				t.Fatalf("IsTransientHTTPError(%q) = %v, want %v", msg, got, want)
			}
		})
	}
}
