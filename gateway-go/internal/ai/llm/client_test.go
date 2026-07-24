package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
	"github.com/choiceoh/deneb/gateway-go/pkg/httputil"
)

// TestClientAppliesUserAgentWithOverride verifies that LLM requests carry the honest
// gateway User-Agent by default, and that a provider config can override
// it (and add extra headers) for endpoints that gate on the client
// identifier.
func TestClientAppliesUserAgentWithOverride(t *testing.T) {
	var gotUA, gotXApp string
	handler := func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotXApp = r.Header.Get("X-App")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}
	drain := func(c *Client) {
		events, err := c.StreamChat(context.Background(), ChatRequest{
			Model:     "m",
			MaxTokens: 16,
			Messages:  []Message{NewTextMessage("user", "hi")},
		})
		if err != nil {
			t.Fatalf("StreamChat: %v", err)
		}
		for range events { //nolint:revive // drain the channel
		}
	}

	// Default: honest gateway User-Agent, no extra headers.
	c1, _ := newTestClient(t, handler, WithAPIMode(APIModeAnthropic))
	drain(c1)
	if gotUA != httputil.UserAgent() {
		t.Errorf("default User-Agent = %q, want %q", gotUA, httputil.UserAgent())
	}
	if gotXApp != "" {
		t.Errorf("unexpected X-App header %q", gotXApp)
	}

	// A provider config can override the User-Agent and add headers.
	c2, _ := newTestClient(t, handler, WithAPIMode(APIModeAnthropic),
		WithHeaders(map[string]string{"User-Agent": "claude-cli/1.0", "X-App": "cli"}))
	drain(c2)
	if gotUA != "claude-cli/1.0" {
		t.Errorf("overridden User-Agent = %q, want claude-cli/1.0", gotUA)
	}
	if gotXApp != "cli" {
		t.Errorf("X-App = %q, want cli", gotXApp)
	}
}

// streamStopHandler responds with a minimal Anthropic SSE stream and
// records the credential headers it received.
func streamStopHandler(xAPIKey, auth *string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		*xAPIKey = r.Header.Get("x-api-key")
		*auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}
}

func drainChat(t *testing.T, c *Client) {
	t.Helper()
	events, err := c.StreamChat(context.Background(), ChatRequest{
		Model:     "m",
		MaxTokens: 16,
		Messages:  []Message{NewTextMessage("user", "hi")},
	})
	if err != nil {
		t.Fatalf("StreamChat: %v", err)
	}
	for range events { //nolint:revive // drain the channel
	}
}

// TestClientAuthSchemeSelectsHeaderFormat verifies the credential goes out as x-api-key by
// default and as Authorization: Bearer when the Bearer scheme is set —
// required by OAuth-token endpoints like Kimi Code.
func TestClientAuthSchemeSelectsHeaderFormat(t *testing.T) {
	var xAPIKey, auth string
	handler := streamStopHandler(&xAPIKey, &auth)

	// Default: x-api-key header, no Authorization.
	xAPIKey, auth = "", ""
	c1, _ := newTestClient(t, handler, WithAPIMode(APIModeAnthropic))
	drainChat(t, c1)
	if xAPIKey != "test-key" {
		t.Errorf("default scheme: x-api-key = %q, want test-key", xAPIKey)
	}
	if auth != "" {
		t.Errorf("default scheme: unexpected Authorization %q", auth)
	}

	// Bearer scheme: Authorization header, no x-api-key.
	xAPIKey, auth = "", ""
	c2, _ := newTestClient(t, handler, WithAPIMode(APIModeAnthropic), WithAuthScheme(AuthSchemeBearer))
	drainChat(t, c2)
	if auth != "Bearer test-key" {
		t.Errorf("bearer scheme: Authorization = %q, want \"Bearer test-key\"", auth)
	}
	if xAPIKey != "" {
		t.Errorf("bearer scheme: unexpected x-api-key %q", xAPIKey)
	}
}

// TestClientAPIKeyFuncRereadsRotatedToken verifies the dynamic key callback overrides the
// static key and is re-read per request, so a rotated token is picked up.
func TestClientAPIKeyFuncRereadsRotatedToken(t *testing.T) {
	var xAPIKey, auth string
	handler := streamStopHandler(&xAPIKey, &auth)

	token := "dynamic-1"
	c, _ := newTestClient(t, handler, WithAPIMode(APIModeAnthropic),
		WithAPIKeyFunc(func() string { return token }))

	drainChat(t, c)
	if xAPIKey != "dynamic-1" {
		t.Errorf("apiKeyFunc: x-api-key = %q, want dynamic-1", xAPIKey)
	}

	// A rotated token is picked up on the next request.
	token = "dynamic-2"
	drainChat(t, c)
	if xAPIKey != "dynamic-2" {
		t.Errorf("apiKeyFunc rotation: x-api-key = %q, want dynamic-2", xAPIKey)
	}
}

// newTestClient creates an httptest server and LLM client for testing.
func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...ClientOption) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.URL, "test-key", opts...), server
}

func TestCloneForDeterministicRunPreservesParentDeadline(t *testing.T) {
	parent := NewClient("http://example.invalid", "test-key", WithMinRequestTimeout(2*time.Second))
	clone := parent.CloneForDeterministicRun()
	if clone == nil {
		t.Fatal("CloneForDeterministicRun returned nil")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	parentDeadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("parent context has no deadline")
	}

	// Prove the source client is configured to extend this short deadline, so
	// equality below is exercising the deterministic clone rather than a weak
	// test setup.
	productionCtx, productionCancel := parent.requestContext(ctx)
	defer productionCancel()
	productionDeadline, ok := productionCtx.Deadline()
	if !ok || !productionDeadline.After(parentDeadline) {
		t.Fatalf("source client did not extend the short deadline: parent=%v production=%v", parentDeadline, productionDeadline)
	}

	deterministicCtx, deterministicCancel := clone.requestContext(ctx)
	defer deterministicCancel()
	deterministicDeadline, ok := deterministicCtx.Deadline()
	if !ok {
		t.Fatal("deterministic request context lost the parent deadline")
	}
	if !deterministicDeadline.Equal(parentDeadline) {
		t.Fatalf("deterministic request deadline = %v, want parent deadline %v", deterministicDeadline, parentDeadline)
	}
}

func TestCloneForDeterministicRunDoesNotRetryHTTP500(t *testing.T) {
	var calls atomic.Int32
	parent, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "transient server failure")
	}, WithRetry(3, time.Millisecond, 5*time.Millisecond))
	clone := parent.CloneForDeterministicRun()

	req, err := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", strings.NewReader(`{"secret":"body"}`))
	if err != nil {
		t.Fatal(err)
	}
	_, err = clone.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected HTTP 500 error")
	}
	var apiErr *httpretry.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusInternalServerError {
		t.Fatalf("error = %v, want APIError status 500", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server calls = %d, want exactly 1 (no deterministic retry)", got)
	}
}

func TestCloneForDeterministicRunRejectsRedirectResponse(t *testing.T) {
	type requestSnapshot struct {
		authorization string
		body          string
	}
	const secretBody = `{"private":"briefcase-secret"}`
	const secretAuthorization = "Bearer briefcase-secret-token"

	targetSeen := make(chan requestSnapshot, 1)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		targetSeen <- requestSnapshot{authorization: r.Header.Get("Authorization"), body: string(body)}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "unexpected redirect target")
	}))
	defer target.Close()

	firstSeen := make(chan requestSnapshot, 1)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		firstSeen <- requestSnapshot{authorization: r.Header.Get("Authorization"), body: string(body)}
		w.Header().Set("Location", target.URL+"/sink")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer first.Close()

	clone := NewClient(first.URL, "unused", WithRetry(3, time.Millisecond, 5*time.Millisecond)).CloneForDeterministicRun()
	req, err := http.NewRequest(http.MethodPost, first.URL+"/start", strings.NewReader(secretBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", secretAuthorization)
	_, err = clone.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected the unfollowed 307 response to be returned as an error")
	}
	var apiErr *httpretry.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("error = %v, want APIError status 307", err)
	}

	select {
	case got := <-firstSeen:
		if got.authorization != secretAuthorization || got.body != secretBody {
			t.Fatalf("first server request = %+v, want original authorization and body", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first server did not receive the request")
	}
	select {
	case got := <-targetSeen:
		t.Fatalf("redirect target received secret request data: %+v", got)
	default:
	}
}

func TestDoStream_Success(t *testing.T) {
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: ping\ndata: {}\n\n")
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body := testutil.Must(c.DoStream(context.Background(), req))
	defer body.Close()
}

func TestDoStream_ClientError_NoRetry(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad request"}`)
	})
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if calls != 1 {
		t.Errorf("got %d, want 1 call (no retry)", calls)
	}
}

func TestDoStream_ServerError_Retries(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}, WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body := testutil.Must(c.DoStream(context.Background(), req))
	defer body.Close()
	if calls != 3 {
		t.Errorf("got %d, want 3 calls", calls)
	}
}

func TestDoStream_ContextCancelled(t *testing.T) {
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "unavailable")
	}, WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(ctx, req)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDoStream_RateLimitRetryAfter(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, "rate limited")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}, WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)

	// Use a context with a generous timeout since retry-after is 1s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body := testutil.Must(c.DoStream(ctx, req))
	defer body.Close()
	if calls != 2 {
		t.Errorf("got %d, want 2 calls", calls)
	}
}

func TestBackoffDelayWithJitterVariesWithinRange(t *testing.T) {
	c := NewClient("http://localhost", "key",
		WithRetry(6, 100*time.Millisecond, 10*time.Second))
	err := &httpretry.APIError{StatusCode: 503, Message: "unavailable"}

	// Run multiple times to verify jitter adds variance.
	seen := make(map[time.Duration]bool)
	for range 20 {
		d := c.backoffDelay(1, err)
		seen[d] = true
		// Base delay for attempt 1: 100ms. Jitter adds 0-25%, so 100-125ms.
		if d < 100*time.Millisecond || d >= 125*time.Millisecond {
			t.Fatalf("delay %v out of expected range [100ms, 125ms)", d)
		}
	}
	if len(seen) < 2 {
		t.Error("expected jitter to produce varying delays")
	}
}

func TestBackoffDelayWithRateLimitFloor(t *testing.T) {
	c := NewClient("http://localhost", "key",
		WithRetry(6, 500*time.Millisecond, 60*time.Second))

	// 429 error should use 2s floor instead of the configured 500ms base.
	rateLimitErr := &httpretry.APIError{StatusCode: 429, Message: "rate limited"}
	d := c.backoffDelay(1, rateLimitErr)
	// Floor is 2s, attempt 1 → 2s * 2^0 = 2s, plus up to 25% jitter → [2s, 2.5s).
	if d < 2*time.Second || d >= 2500*time.Millisecond {
		t.Fatalf("429 delay %v out of expected range [2s, 2.5s)", d)
	}

	// 503 error should use the configured 500ms base (no floor).
	serverErr := &httpretry.APIError{StatusCode: 503, Message: "unavailable"}
	d = c.backoffDelay(1, serverErr)
	// 500ms * 2^0 = 500ms, plus up to 25% jitter → [500ms, 625ms).
	if d < 500*time.Millisecond || d >= 625*time.Millisecond {
		t.Fatalf("503 delay %v out of expected range [500ms, 625ms)", d)
	}
}

// TestParseRetryAfter: both RFC 9110 forms parse — delay-seconds and
// HTTP-date — with surrounding whitespace tolerated; garbage and past dates
// yield 0 (fall back to exponential backoff).
func TestParseRetryAfter(t *testing.T) {
	if got := parseRetryAfter("120"); got != 120*time.Second {
		t.Errorf("seconds form = %v, want 120s", got)
	}
	if got := parseRetryAfter(" 120 "); got != 120*time.Second {
		t.Errorf("OWS-wrapped seconds = %v, want 120s", got)
	}
	future := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(future); got <= 80*time.Second || got > 90*time.Second {
		t.Errorf("HTTP-date form = %v, want ~90s", got)
	}
	if got := parseRetryAfter(future + "\r\n"); got <= 80*time.Second || got > 90*time.Second {
		t.Errorf("CRLF-tailed HTTP-date = %v, want ~90s", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	for _, val := range []string{"", "soon", "-5", past} {
		if got := parseRetryAfter(val); got != 0 {
			t.Errorf("parseRetryAfter(%q) = %v, want 0", val, got)
		}
	}
}

// TestBackoffDelay_RetryAfterClamp: a provider-directed Retry-After larger
// than the configured max delay is clamped so it cannot stall the retry loop;
// values within the max are honored verbatim.
func TestBackoffDelay_RetryAfterClamp(t *testing.T) {
	c := NewClient("http://localhost", "key",
		WithRetry(6, 100*time.Millisecond, 10*time.Second))

	huge := &httpretry.APIError{StatusCode: 429, Message: "rate limited", RetryAfter: time.Hour}
	if got := c.backoffDelay(1, huge); got != 10*time.Second {
		t.Errorf("oversized Retry-After = %v, want clamp to 10s", got)
	}
	modest := &httpretry.APIError{StatusCode: 429, Message: "rate limited", RetryAfter: 3 * time.Second}
	if got := c.backoffDelay(1, modest); got != 3*time.Second {
		t.Errorf("in-range Retry-After = %v, want 3s honored verbatim", got)
	}
}

func TestDoStream_DefaultMaxRetries(t *testing.T) {
	calls := 0
	// Use default client (maxRetries=6) with fast delays for testing. A 503 (not
	// a 429) exercises the general retry path — rate-limit 429s have their own
	// lower cap (see TestDoStream_RateLimit_GivesUpEarlyForFailover).
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "unavailable")
	}, WithRetry(6, 1*time.Millisecond, 10*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 1 initial + 6 retries = 7 total calls.
	if calls != 7 {
		t.Errorf("got %d, want 7 calls (1 + 6 retries)", calls)
	}
}

func TestDoStream_504_Retries(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			fmt.Fprint(w, "gateway timeout")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}, WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body := testutil.Must(c.DoStream(context.Background(), req))
	defer body.Close()
	if calls != 2 {
		t.Errorf("got %d, want 2 calls (1 timeout + 1 success)", calls)
	}
}

func TestDoStream_429Code1302_NoRetry(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"code":"1302","message":"Rate limit reached for requests"}}`)
	}, WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 429 code 1302 response")
	}
	if calls != 1 {
		t.Errorf("got %d, want 1 call (no retry on provider hard rate-limit)", calls)
	}
}

func TestDoStream_ExpiredContext_MinRequestTimeout(t *testing.T) {
	// The parent context is already past its deadline, but minRequestTimeout
	// should give the HTTP request a fresh timeout so it can still succeed.
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: ok\n\n")
	}, WithMinRequestTimeout(5*time.Second), WithRetry(0, 0, 0))

	// Create a context with a deadline that has already passed.
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Millisecond))
	defer cancel()
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		t.Fatalf("context error = %v, want deadline exceeded", ctx.Err())
	}

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", nil)
	body := testutil.Must(c.DoStream(ctx, req))
	defer body.Close()
}

func TestDoStream_MinRequestTimeout_ParentDeadlineDoesNotCancelRequest(t *testing.T) {
	// A too-short parent deadline should not cancel the fresh per-request
	// timeout. Explicit parent cancellation is covered by the next test.
	reqReceived := make(chan struct{})
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(reqReceived)
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: ok\n\n")
	}, WithMinRequestTimeout(500*time.Millisecond), WithRetry(0, 0, 0))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", nil)

	done := make(chan error, 1)
	go func() {
		body, err := c.DoStream(ctx, req)
		if err == nil {
			err = body.Close()
		}
		done <- err
	}()

	<-reqReceived
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("DoStream returned error after parent deadline: %v", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("DoStream did not complete within min request timeout")
	}
}

func TestDoStream_MinRequestTimeout_ParentCancelPropagates(t *testing.T) {
	// When the parent context is explicitly cancelled (agent abort) while
	// the request is in flight, the derived request context should also
	// be cancelled — even though minRequestTimeout gave it a fresh deadline.
	reqReceived := make(chan struct{})
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		close(reqReceived)
		// Block until the request context is cancelled.
		<-r.Context().Done()
	}, WithMinRequestTimeout(30*time.Second), WithRetry(0, 0, 0))

	// Parent has a short deadline (triggers minRequestTimeout) but is NOT
	// yet expired — so AfterFunc can propagate the explicit cancel.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", nil)

	done := make(chan error, 1)
	go func() {
		_, err := c.DoStream(ctx, req)
		done <- err
	}()

	// Wait for the server to receive the request, then cancel parent.
	<-reqReceived
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error after parent cancel")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DoStream did not return after parent cancel")
	}
}

func TestDoStream_ExpiredContext_NoRetry(t *testing.T) {
	// When the parent context is expired, retries should be skipped.
	calls := 0
	// Disable minRequestTimeout so the expired context is not rescued.
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "unavailable")
	}, WithMinRequestTimeout(0), WithRetry(3, 500*time.Millisecond, 1*time.Second))

	// Use a context that expires after the first request completes.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", nil)
	_, err := c.DoStream(ctx, req)
	if err == nil {
		t.Fatal("expected error")
	}
	// Should have made 1 call (the initial), then context expires during/before retry delay.
	if calls > 2 {
		t.Errorf("got %d, want at most 2 calls (context should expire before retries)", calls)
	}
}

func TestDoStream_429OtherCode_Retries(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"code":"9999","message":"temporary rate limit"}}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}, WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body := testutil.Must(c.DoStream(context.Background(), req))
	defer body.Close()
	if calls != 3 {
		t.Errorf("got %d, want 3 calls for retryable 429 payload", calls)
	}
}

// A persistently overloaded provider (429) must be abandoned after
// rateLimitMaxRetries — well short of the full maxRetries — so the caller's
// model-fallback chain can switch models before the turn budget is spent.
func TestDoStream_RateLimit_GivesUpEarlyForFailover(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"type":"rate_limit_error","message":"The engine is currently overloaded"}}`)
	}, WithRetry(6, time.Millisecond, 5*time.Millisecond))
	c.rateLimitMaxRetries = 2 // give up after 2 rate-limit retries (3 calls)

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected the 429 to surface after early giveup")
	}
	if calls != c.rateLimitMaxRetries+1 {
		t.Errorf("got %d calls, want %d (initial + %d rate-limit retries, NOT the full maxRetries=6)",
			calls, c.rateLimitMaxRetries+1, c.rateLimitMaxRetries)
	}
}
