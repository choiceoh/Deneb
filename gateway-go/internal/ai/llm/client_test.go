package llm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// newTestClient creates an httptest server and LLM client for testing.
func newTestClient(t *testing.T, handler http.HandlerFunc, opts ...ClientOption) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(server.URL, "test-key", opts...), server
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

func TestBackoffDelay_Jitter(t *testing.T) {
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

func TestBackoffDelay_RateLimitFloor(t *testing.T) {
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

func TestDoStream_DefaultMaxRetries(t *testing.T) {
	calls := 0
	// Use default client (maxRetries=6) with fast delays for testing.
	c, server := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprint(w, "rate limited")
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
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond) // ensure deadline passes

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/chat/completions", nil)
	body := testutil.Must(c.DoStream(ctx, req))
	defer body.Close()
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
