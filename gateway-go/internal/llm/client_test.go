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

func TestAPIError_Error(t *testing.T) {
	err := &APIError{StatusCode: 429, Body: "rate limited"}
	want := "LLM API error 429: rate limited"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestIsRetryable(t *testing.T) {
	tests := []struct {
		status    int
		retryable bool
	}{
		{200, false},
		{400, false},
		{401, false},
		{403, false},
		{429, true},
		{500, true},
		{502, true},
		{503, true},
		{529, true},
	}
	for _, tt := range tests {
		if got := isRetryable(tt.status); got != tt.retryable {
			t.Errorf("isRetryable(%d) = %v, want %v", tt.status, got, tt.retryable)
		}
	}
}
