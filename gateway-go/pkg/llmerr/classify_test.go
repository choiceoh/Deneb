package llmerr

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// ─── Classify: happy paths ──────────────────────────────────────────────

func TestClassify_ByStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   []byte
		want   Reason
	}{
		{"401 -> auth", 401, nil, ReasonAuth},
		{"403 plain -> auth", 403, nil, ReasonAuth},
		{
			"403 with key-limit -> billing",
			403,
			[]byte(`{"error":{"message":"key limit exceeded"}}`),
			ReasonBilling,
		},
		{"402 plain -> billing", 402, nil, ReasonBilling},
		{
			"402 with try-again -> rate limit",
			402,
			[]byte(`{"error":{"message":"usage limit, try again in 5 minutes"}}`),
			ReasonRateLimit,
		},
		{"404 plain -> unknown (retryable)", 404, nil, ReasonUnknown},
		{
			"404 model not found -> model_not_found",
			404,
			[]byte(`{"error":{"message":"model not found"}}`),
			ReasonModelNotFound,
		},
		{"413 -> payload_too_large", 413, nil, ReasonPayloadTooLarge},
		{"429 -> rate_limit", 429, nil, ReasonRateLimit},
		{"500 -> server_error", 500, nil, ReasonServerError},
		{"502 -> server_error", 502, nil, ReasonServerError},
		{"503 -> overloaded", 503, nil, ReasonOverloaded},
		{"529 -> overloaded", 529, nil, ReasonOverloaded},
		{"418 -> format_error (other 4xx)", 418, nil, ReasonFormatError},
		{"599 -> server_error (other 5xx)", 599, nil, ReasonServerError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(nil, tc.status, tc.body)
			if got.Reason != tc.want {
				t.Fatalf("Reason = %v, want %v", got.Reason, tc.want)
			}
			if got.HTTPStatus != tc.status {
				t.Fatalf("HTTPStatus = %d, want %d", got.HTTPStatus, tc.status)
			}
		})
	}
}

func TestClassify_ProviderSpecific(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   []byte
		want   Reason
	}{
		{
			"thinking signature invalid",
			400,
			[]byte(`{"error":{"message":"invalid signature for thinking block"}}`),
			ReasonThinkingSignature,
		},
		{
			"long context tier gate",
			429,
			[]byte(`{"error":{"message":"extra usage required for long context"}}`),
			ReasonLongContextTier,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(nil, tc.status, tc.body)
			if got.Reason != tc.want {
				t.Fatalf("Reason = %v, want %v", got.Reason, tc.want)
			}
		})
	}
}

func TestClassify_By400ContextOverflow(t *testing.T) {
	body := []byte(`{"error":{"message":"This model's maximum context length is 128000 tokens"}}`)
	got := Classify(nil, 400, body)
	if got.Reason != ReasonContextOverflow {
		t.Fatalf("Reason = %v, want context_overflow", got.Reason)
	}
}

func TestClassify_By400GenericLargeSession(t *testing.T) {
	// Bare "Error" body with a large session → overflow heuristic.
	body := []byte(`{"error":{"message":"Error"}}`)
	got := ClassifyWithHint(nil, 400, body, Hint{TokenUsagePercent: 0.75})
	if got.Reason != ReasonContextOverflow {
		t.Fatalf("Reason = %v, want context_overflow (large session heuristic)", got.Reason)
	}
}

func TestClassify_By400GenericSmallSession(t *testing.T) {
	body := []byte(`{"error":{"message":"Error"}}`)
	// No hint → not large → plain format error.
	got := Classify(nil, 400, body)
	if got.Reason != ReasonFormatError {
		t.Fatalf("Reason = %v, want format_error", got.Reason)
	}
}

func TestClassify_ByErrorCode(t *testing.T) {
	tests := []struct {
		name string
		body []byte
		want Reason
	}{
		{
			"insufficient_quota",
			[]byte(`{"error":{"code":"insufficient_quota","message":"You exceeded your current quota"}}`),
			ReasonBilling,
		},
		{
			"rate_limit_exceeded code",
			[]byte(`{"error":{"code":"rate_limit_exceeded"}}`),
			ReasonRateLimit,
		},
		{
			"context_length_exceeded",
			[]byte(`{"error":{"code":"context_length_exceeded"}}`),
			ReasonContextOverflow,
		},
		{
			"invalid_model code",
			[]byte(`{"error":{"code":"invalid_model"}}`),
			ReasonModelNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(nil, 0, tc.body)
			if got.Reason != tc.want {
				t.Fatalf("Reason = %v, want %v", got.Reason, tc.want)
			}
			if got.ProviderCode == "" {
				t.Fatalf("ProviderCode empty, want populated")
			}
		})
	}
}

func TestClassify_ByMessageOnly(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want Reason
	}{
		{"rate limit", errors.New("Rate limit exceeded for this model"), ReasonRateLimit},
		{"insufficient quota", errors.New("insufficient_quota: please top up"), ReasonBilling},
		{"context length", errors.New("prompt is too long for this model"), ReasonContextOverflow},
		{"invalid api key", errors.New("Invalid API key provided"), ReasonAuth},
		{"model not found (msg)", errors.New("The model 'gpt-5' does not exist"), ReasonModelNotFound},
		{"overloaded (msg)", errors.New("Service is overloaded, rate limit"), ReasonRateLimit},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Classify(tc.err, 0, nil)
			if got.Reason != tc.want {
				t.Fatalf("Reason = %v, want %v (msg=%q)", got.Reason, tc.want, tc.err.Error())
			}
		})
	}
}

// TestClassify_LegacyContextOverflowStrings guards the migration of
// chat/run_helpers.go:isContextOverflow from a hand-rolled substring list
// to llmerr.Classify. Every string the old implementation matched must
// still classify as ReasonContextOverflow.
func TestClassify_LegacyContextOverflowStrings(t *testing.T) {
	// Exact strings from the pre-migration substring check.
	legacy := []string{
		"context_length_exceeded",
		"context_too_long",
		"prompt is too long",
		"maximum context length",
	}
	for _, s := range legacy {
		t.Run(s, func(t *testing.T) {
			got := Classify(errors.New(s), 0, nil)
			if got.Reason != ReasonContextOverflow {
				t.Fatalf("Classify(%q).Reason = %v, want context_overflow", s, got.Reason)
			}
		})
	}
}

func TestClassify_OpenRouterWrappedRawMetadata(t *testing.T) {
	// OpenRouter wraps upstream errors inside metadata.raw.
	body := []byte(`{"error":{"message":"Provider returned error","metadata":{"raw":"{\"error\":{\"message\":\"maximum context length exceeded\"}}"}}}`)
	got := Classify(nil, 200, body) // note: 2xx, matched by message
	// 200 won't match any status; should fall through to message match.
	// Expect overflow because inner raw string mentions "maximum context".
	if got.Reason != ReasonContextOverflow {
		t.Fatalf("Reason = %v, want context_overflow (OpenRouter wrap)", got.Reason)
	}
}

// ─── Transport / timeout ────────────────────────────────────────────────

type fakeNetTimeoutError struct{}

func (fakeNetTimeoutError) Error() string   { return "i/o timeout" }
func (fakeNetTimeoutError) Timeout() bool   { return true }
func (fakeNetTimeoutError) Temporary() bool { return true }

func TestClassify_TransportTimeout(t *testing.T) {
	got := Classify(fakeNetTimeoutError{}, 0, nil)
	if got.Reason != ReasonTimeout {
		t.Fatalf("Reason = %v, want timeout", got.Reason)
	}
}

func TestClassify_DNSTimeout(t *testing.T) {
	dnsErr := &net.DNSError{Err: "i/o timeout", IsTimeout: true}
	got := Classify(dnsErr, 0, nil)
	if got.Reason != ReasonTimeout {
		t.Fatalf("Reason = %v, want timeout (DNS)", got.Reason)
	}
}

func TestClassify_ContextDeadline(t *testing.T) {
	got := Classify(context.DeadlineExceeded, 0, nil)
	if got.Reason != ReasonTimeout {
		t.Fatalf("Reason = %v, want timeout (ctx deadline)", got.Reason)
	}
}

func TestClassify_ContextCanceled(t *testing.T) {
	got := Classify(context.Canceled, 0, nil)
	if got.Reason != ReasonTimeout {
		t.Fatalf("Reason = %v, want timeout (ctx canceled)", got.Reason)
	}
}

func TestClassify_DisconnectSmallSession(t *testing.T) {
	got := Classify(errors.New("server disconnected without sending a response"), 0, nil)
	if got.Reason != ReasonTimeout {
		t.Fatalf("Reason = %v, want timeout (small-session disconnect)", got.Reason)
	}
}

func TestClassify_DisconnectLargeSession(t *testing.T) {
	err := errors.New("peer closed connection without sending complete response")
	got := ClassifyWithHint(err, 0, nil, Hint{ApproxTokens: 150_000})
	if got.Reason != ReasonContextOverflow {
		t.Fatalf("Reason = %v, want context_overflow (large-session disconnect)", got.Reason)
	}
}

func TestClassify_SSLTransient(t *testing.T) {
	got := Classify(errors.New("remote error: tls: bad record mac"), 0, nil)
	if got.Reason != ReasonTimeout {
		t.Fatalf("Reason = %v, want timeout (SSL transient)", got.Reason)
	}
}

func TestClassify_SSLTransientOverridesLargeSession(t *testing.T) {
	// SSL hiccup on a large session should NOT trigger compression.
	err := errors.New("bad_record_mac on stream")
	got := ClassifyWithHint(err, 0, nil, Hint{ApproxTokens: 150_000})
	if got.Reason != ReasonTimeout {
		t.Fatalf("Reason = %v, want timeout (SSL supersedes large-session)", got.Reason)
	}
}

// ─── Edge cases ─────────────────────────────────────────────────────────

func TestClassify_NilInputs(t *testing.T) {
	got := Classify(nil, 0, nil)
	if got.Reason != ReasonUnknown {
		t.Fatalf("Reason = %v, want unknown on nil inputs", got.Reason)
	}
	if got.Err != nil {
		t.Fatalf("Err = %v, want nil", got.Err)
	}
}

func TestClassify_EmptyBody(t *testing.T) {
	got := Classify(nil, 429, []byte{})
	if got.Reason != ReasonRateLimit {
		t.Fatalf("Reason = %v, want rate_limit", got.Reason)
	}
}

func TestClassify_MalformedBody(t *testing.T) {
	got := Classify(errors.New("rate limit"), 0, []byte("not json <html>"))
	if got.Reason != ReasonRateLimit {
		t.Fatalf("Reason = %v, want rate_limit (fallback to msg)", got.Reason)
	}
}

func TestClassify_PreservesOriginalError(t *testing.T) {
	sentinel := errors.New("sentinel")
	got := Classify(sentinel, 503, nil)
	if !errors.Is(got.Err, sentinel) {
		t.Fatalf("Err = %v, want to wrap sentinel", got.Err)
	}
}

func TestClassify_MessageTruncation(t *testing.T) {
	huge := make([]byte, 0, 1200)
	for range 1200 {
		huge = append(huge, 'a')
	}
	got := Classify(errors.New(string(huge)), 0, nil)
	if len(got.Message) > 500 {
		t.Fatalf("Message len = %d, want <= 500", len(got.Message))
	}
}

// ─── Reason metadata ────────────────────────────────────────────────────

func TestReason_String(t *testing.T) {
	cases := map[Reason]string{
		ReasonUnknown:           "unknown",
		ReasonAuth:              "auth",
		ReasonAuthPermanent:     "auth_permanent",
		ReasonBilling:           "billing",
		ReasonRateLimit:         "rate_limit",
		ReasonOverloaded:        "overloaded",
		ReasonServerError:       "server_error",
		ReasonTimeout:           "timeout",
		ReasonContextOverflow:   "context_overflow",
		ReasonPayloadTooLarge:   "payload_too_large",
		ReasonModelNotFound:     "model_not_found",
		ReasonFormatError:       "format_error",
		ReasonThinkingSignature: "thinking_signature",
		ReasonLongContextTier:   "long_context_tier",
	}
	for r, want := range cases {
		if got := r.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(r), got, want)
		}
	}
	// Unknown out-of-range value.
	if got := Reason(9999).String(); got != "unknown" {
		t.Errorf("Reason(9999).String() = %q, want unknown", got)
	}
}

func TestReason_Retryable(t *testing.T) {
	notRetryable := []Reason{
		ReasonAuthPermanent,
		ReasonBilling,
		ReasonFormatError,
		ReasonModelNotFound,
	}
	for _, r := range notRetryable {
		if r.Retryable() {
			t.Errorf("%v.Retryable() = true, want false", r)
		}
	}
	retryable := []Reason{
		ReasonUnknown,
		ReasonAuth,
		ReasonRateLimit,
		ReasonOverloaded,
		ReasonServerError,
		ReasonTimeout,
		ReasonContextOverflow,
		ReasonPayloadTooLarge,
		ReasonThinkingSignature,
		ReasonLongContextTier,
	}
	for _, r := range retryable {
		if !r.Retryable() {
			t.Errorf("%v.Retryable() = false, want true", r)
		}
	}
}

// ─── Action matrix ──────────────────────────────────────────────────────

func TestDefaultAction_Matrix(t *testing.T) {
	tests := []struct {
		reason Reason
		check  func(a Action) string // returns "" on ok, error msg otherwise
	}{
		{ReasonAuth, func(a Action) string {
			if !a.Rotate || !a.Refresh || !a.RetryOnce {
				return "want Rotate+Refresh+RetryOnce"
			}
			return ""
		}},
		{ReasonAuthPermanent, func(a Action) string {
			if !a.Abort || a.Backoff != 0 {
				return "want Abort only"
			}
			return ""
		}},
		{ReasonBilling, func(a Action) string {
			if !a.Rotate || !a.Abort {
				return "want Rotate+Abort"
			}
			return ""
		}},
		{ReasonRateLimit, func(a Action) string {
			if !a.Rotate || a.Backoff <= 0 {
				return "want Rotate+Backoff"
			}
			return ""
		}},
		{ReasonOverloaded, func(a Action) string {
			if a.Backoff <= 0 || a.Abort {
				return "want Backoff only"
			}
			return ""
		}},
		{ReasonServerError, func(a Action) string {
			if a.Backoff <= 0 || a.Abort {
				return "want Backoff only"
			}
			return ""
		}},
		{ReasonTimeout, func(a Action) string {
			if a.Backoff <= 0 {
				return "want Backoff"
			}
			return ""
		}},
		{ReasonContextOverflow, func(a Action) string {
			if !a.Compress {
				return "want Compress"
			}
			return ""
		}},
		{ReasonPayloadTooLarge, func(a Action) string {
			if !a.Compress {
				return "want Compress"
			}
			return ""
		}},
		{ReasonModelNotFound, func(a Action) string {
			if !a.Abort {
				return "want Abort"
			}
			return ""
		}},
		{ReasonFormatError, func(a Action) string {
			if !a.Abort {
				return "want Abort"
			}
			return ""
		}},
		{ReasonThinkingSignature, func(a Action) string {
			if !a.StripThink || !a.RetryOnce {
				return "want StripThink+RetryOnce"
			}
			return ""
		}},
		{ReasonLongContextTier, func(a Action) string {
			if !a.Compress || a.Backoff <= 0 {
				return "want Compress+Backoff"
			}
			return ""
		}},
		{ReasonUnknown, func(a Action) string {
			if a.Backoff <= 0 {
				return "want Backoff"
			}
			return ""
		}},
	}
	for _, tc := range tests {
		t.Run(tc.reason.String(), func(t *testing.T) {
			if msg := tc.check(tc.reason.DefaultAction(1)); msg != "" {
				t.Fatalf("DefaultAction(%v): %s", tc.reason, msg)
			}
		})
	}
}

func TestDefaultAction_BackoffGrowsAndCaps(t *testing.T) {
	a1 := ReasonServerError.DefaultAction(1)
	a2 := ReasonServerError.DefaultAction(2)
	a10 := ReasonServerError.DefaultAction(10)
	if a1.Backoff != BaseBackoff {
		t.Errorf("attempt 1 Backoff = %v, want %v", a1.Backoff, BaseBackoff)
	}
	if a2.Backoff <= a1.Backoff {
		t.Errorf("backoff did not grow: a1=%v a2=%v", a1.Backoff, a2.Backoff)
	}
	if a10.Backoff != MaxBackoff {
		t.Errorf("attempt 10 Backoff = %v, want cap %v", a10.Backoff, MaxBackoff)
	}
	// Very large attempt should still return MaxBackoff without overflow.
	if got := ReasonServerError.DefaultAction(1000).Backoff; got != MaxBackoff {
		t.Errorf("attempt 1000 Backoff = %v, want %v", got, MaxBackoff)
	}
}

func TestDefaultAction_AttemptClampedPositive(t *testing.T) {
	a := ReasonServerError.DefaultAction(0)
	if a.Backoff != BaseBackoff {
		t.Errorf("attempt 0 should clamp to 1: Backoff = %v, want %v", a.Backoff, BaseBackoff)
	}
	a = ReasonServerError.DefaultAction(-5)
	if a.Backoff != BaseBackoff {
		t.Errorf("attempt -5 should clamp to 1: Backoff = %v, want %v", a.Backoff, BaseBackoff)
	}
}

// ─── Sanity: backoffFor is deterministic ───────────────────────────────

func TestBackoffFor_Deterministic(t *testing.T) {
	for i := 1; i < 20; i++ {
		first := backoffFor(i)
		second := backoffFor(i)
		if first != second {
			t.Fatalf("backoffFor(%d) nondeterministic: %v vs %v", i, first, second)
		}
	}
	if backoffFor(1) != 500*time.Millisecond {
		t.Fatalf("backoffFor(1) = %v, want 500ms", backoffFor(1))
	}
}
