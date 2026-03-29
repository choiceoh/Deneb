package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDoStream_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: ping\ndata: {}\n\n")
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body, err := c.DoStream(context.Background(), req)
	if err != nil {
		t.Fatalf("DoStream error: %v", err)
	}
	defer body.Close()
}

func TestDoStream_ClientError_NoRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad request"}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 400 response")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry), got %d", calls)
	}
}

func TestDoStream_ServerError_Retries(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprint(w, "unavailable")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body, err := c.DoStream(context.Background(), req)
	if err != nil {
		t.Fatalf("DoStream error: %v", err)
	}
	defer body.Close()
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestDoStream_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		fmt.Fprint(w, "unavailable")
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(ctx, req)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestDoStream_RateLimitRetryAfter(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, "rate limited")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)

	// Use a context with a generous timeout since retry-after is 1s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	body, err := c.DoStream(ctx, req)
	if err != nil {
		t.Fatalf("DoStream error: %v", err)
	}
	defer body.Close()
	if calls != 2 {
		t.Errorf("expected 2 calls, got %d", calls)
	}
}

func TestBackoffDelay_Jitter(t *testing.T) {
	c := NewClient("http://localhost", "key",
		WithRetry(6, 100*time.Millisecond, 10*time.Second))
	err := &APIError{StatusCode: 503, Body: "unavailable"}

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

func TestBackoffDelay_RateLimitFloor(t *testing.T) {
	c := NewClient("http://localhost", "key",
		WithRetry(6, 500*time.Millisecond, 60*time.Second))

	// 429 error should use 2s floor instead of the configured 500ms base.
	rateLimitErr := &APIError{StatusCode: 429, Body: "rate limited"}
	d := c.backoffDelay(1, rateLimitErr)
	// Floor is 2s, attempt 1 → 2s * 2^0 = 2s, plus up to 25% jitter → [2s, 2.5s).
	if d < 2*time.Second || d >= 2500*time.Millisecond {
		t.Fatalf("429 delay %v out of expected range [2s, 2.5s)", d)
	}

	// 503 error should use the configured 500ms base (no floor).
	serverErr := &APIError{StatusCode: 503, Body: "unavailable"}
	d = c.backoffDelay(1, serverErr)
	// 500ms * 2^0 = 500ms, plus up to 25% jitter → [500ms, 625ms).
	if d < 500*time.Millisecond || d >= 625*time.Millisecond {
		t.Fatalf("503 delay %v out of expected range [500ms, 625ms)", d)
	}
}

func TestDoStream_DefaultMaxRetries(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "rate limited")
	}))
	defer server.Close()

	// Use default client (maxRetries=6) with fast delays for testing.
	c := NewClient(server.URL, "test-key",
		WithRetry(6, 1*time.Millisecond, 10*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	// 1 initial + 6 retries = 7 total calls.
	if calls != 7 {
		t.Errorf("expected 7 calls (1 + 6 retries), got %d", calls)
	}
}

func TestAPIError_Error(t *testing.T) {
	err := &APIError{StatusCode: 429, Body: "rate limited"}
	want := "LLM API error 429: rate limited"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestDoStream_504_Retries(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusGatewayTimeout)
			fmt.Fprint(w, "gateway timeout")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body, err := c.DoStream(context.Background(), req)
	if err != nil {
		t.Fatalf("DoStream error: %v", err)
	}
	defer body.Close()
	if calls != 2 {
		t.Errorf("expected 2 calls (1 timeout + 1 success), got %d", calls)
	}
}

func TestDoStream_410_NoRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusGone)
		fmt.Fprint(w, "gone")
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 410 response")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on 410 Gone), got %d", calls)
	}
}

func TestDoStream_501_NoRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusNotImplemented)
		fmt.Fprint(w, "not implemented")
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 501 response")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on 501 Not Implemented), got %d", calls)
	}
}

func TestDoStream_429Code1302_NoRetry(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, `{"error":{"code":"1302","message":"Rate limit reached for requests"}}`)
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	_, err := c.DoStream(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for 429 code 1302 response")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (no retry on provider hard rate-limit), got %d", calls)
	}
}

func TestDoStream_429OtherCode_Retries(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"code":"9999","message":"temporary rate limit"}}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key",
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body, err := c.DoStream(context.Background(), req)
	if err != nil {
		t.Fatalf("DoStream error: %v", err)
	}
	defer body.Close()
	if calls != 3 {
		t.Errorf("expected 3 calls for retryable 429 payload, got %d", calls)
	}
}
