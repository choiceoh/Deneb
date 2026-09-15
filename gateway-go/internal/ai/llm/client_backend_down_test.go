package llm

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/infra/httpretry"
)

// The liveness gate exists because a refusing engine turned every call into a
// full retry ladder: six attempts, about 70 seconds, for an answer that was
// never coming (2026-09-14). These tests pin the three places the gate must
// act — before the first attempt, during a backoff, and not at all when it has
// nothing to say.

func newGatedRequest(t *testing.T, url string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url+"/chat/completions", strings.NewReader(`{"model":"m"}`))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestDoStream_BackendDownRefusesBeforeFirstAttempt(t *testing.T) {
	var calls atomic.Int32
	client, server := newTestClient(
		t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		},
		WithRetry(6, time.Second, 35*time.Second),
		WithBackendDownCheck(func(model string) bool { return model == "m-dead" }),
	)

	start := time.Now()
	_, err := client.doStream(context.Background(), newGatedRequest(t, server.URL), "m-dead")
	if !errors.Is(err, ErrBackendDown) {
		t.Fatalf("err = %v, want ErrBackendDown", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("server calls = %d, want 0: an engine already reported down is not worth one request", got)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("refusal took %v, want immediate", elapsed)
	}
	// Nothing transient may be wrapped: a 502 inside would classify as
	// retryable and invite the replay the gate exists to stop.
	var apiErr *httpretry.APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("refusal wraps an APIError (status %d); it must wrap nothing retryable", apiErr.StatusCode)
	}
}

func TestDoStream_BackendDownStopsRetryAfterFailedAttempt(t *testing.T) {
	var (
		calls atomic.Int32
		down  atomic.Bool
	)
	client, server := newTestClient(
		t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			// Reported down before the client decides to retry.
			down.Store(true)
			w.WriteHeader(http.StatusBadGateway)
		},
		WithRetry(6, 4*time.Second, 35*time.Second),
		WithBackendDownCheck(func(string) bool { return down.Load() }),
	)

	start := time.Now()
	_, err := client.doStream(context.Background(), newGatedRequest(t, server.URL), "m-local")
	if !errors.Is(err, ErrBackendDown) {
		t.Fatalf("err = %v, want ErrBackendDown", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server calls = %d, want 1", got)
	}
	if elapsed := time.Since(start); elapsed > 500*time.Millisecond {
		t.Fatalf("returned after %v: a model reported down before the retry must not wait out a backoff", elapsed)
	}
}

func TestDoStream_BackendDownEndsRetryBackoffEarly(t *testing.T) {
	var (
		calls atomic.Int32
		down  atomic.Bool
	)
	client, server := newTestClient(
		t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			// The probe notices the outage only once the client is already asleep
			// in its first backoff — the case the in-sleep recheck exists for. The
			// report must land AFTER the retry decision, or the loop-head check
			// answers first and this test proves nothing about the sleep.
			time.AfterFunc(300*time.Millisecond, func() { down.Store(true) })
			w.WriteHeader(http.StatusBadGateway)
		},
		// Base 4s with 25% jitter: the first backoff is at least 3 seconds.
		WithRetry(6, 4*time.Second, 35*time.Second),
		WithBackendDownCheck(func(string) bool { return down.Load() }),
	)

	start := time.Now()
	_, err := client.doStream(context.Background(), newGatedRequest(t, server.URL), "m-local")
	elapsed := time.Since(start)
	if !errors.Is(err, ErrBackendDown) {
		t.Fatalf("err = %v, want ErrBackendDown", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("server calls = %d, want 1 (no retry once reported down)", got)
	}
	if elapsed < 300*time.Millisecond {
		t.Fatalf("returned after %v, before the down report existed: the loop-head check answered, the sleep path was not exercised", elapsed)
	}
	if elapsed >= 2500*time.Millisecond {
		t.Fatalf("returned after %v; the gate is rechecked every %v, so a down report must end a ≥3s backoff early", elapsed, backendRecheck)
	}
}

func TestDoStream_BackendDownGateIgnoresOtherModelsAndUnknownModel(t *testing.T) {
	var calls atomic.Int32
	client, server := newTestClient(
		t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		},
		WithRetry(2, time.Millisecond, 2*time.Millisecond),
		WithBackendDownCheck(func(model string) bool { return model == "m-dead" }),
	)

	// A healthy model keeps its full retry ladder.
	_, err := client.doStream(context.Background(), newGatedRequest(t, server.URL), "m-cloud")
	if errors.Is(err, ErrBackendDown) {
		t.Fatalf("err = %v: the gate refused a model it does not report down", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("server calls = %d, want 3 (1 + 2 retries)", got)
	}

	// DoStream carries no model, so it is never gated.
	calls.Store(0)
	if _, err := client.DoStream(context.Background(), newGatedRequest(t, server.URL)); errors.Is(err, ErrBackendDown) {
		t.Fatalf("DoStream err = %v: a request without a model must not be gated", err)
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("DoStream server calls = %d, want 3", got)
	}
}

func TestStreamChat_CarriesModelToBackendDownGate(t *testing.T) {
	var calls atomic.Int32
	client, _ := newTestClient(
		t, func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusBadGateway)
		},
		WithBackendDownCheck(func(model string) bool { return model == "m-dead" }),
	)

	_, err := client.StreamChat(context.Background(), ChatRequest{
		Model:    "m-dead",
		Messages: []Message{NewTextMessage("user", "hi")},
	})
	if !errors.Is(err, ErrBackendDown) {
		t.Fatalf("StreamChat err = %v, want ErrBackendDown", err)
	}
	if _, err := client.Complete(context.Background(), ChatRequest{
		Model:    "m-dead",
		Messages: []Message{NewTextMessage("user", "hi")},
	}); !errors.Is(err, ErrBackendDown) {
		t.Fatalf("Complete err = %v, want ErrBackendDown", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("server calls = %d, want 0", got)
	}
}

func TestCloneForDeterministicRunKeepsBackendDownGate(t *testing.T) {
	var calls atomic.Int32
	parent, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}, WithBackendDownCheck(func(string) bool { return true }))
	clone := parent.CloneForDeterministicRun()

	if _, err := clone.doStream(context.Background(), newGatedRequest(t, server.URL), "m"); !errors.Is(err, ErrBackendDown) {
		t.Fatalf("clone err = %v, want ErrBackendDown — a clone must not lose the gate", err)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("server calls = %d, want 0", got)
	}
}
