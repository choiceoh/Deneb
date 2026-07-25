package llm

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// The summary is grouped on downstream, so the same failure mix must always
// render the same string regardless of arrival order.
func TestRetryCollectorSummaryIsDeterministic(t *testing.T) {
	a := &RetryCollector{}
	a.record(retryAttempt{status: 429})
	a.record(retryAttempt{kind: "transport"})
	a.record(retryAttempt{status: 429})

	b := &RetryCollector{}
	b.record(retryAttempt{kind: "transport"})
	b.record(retryAttempt{status: 429})
	b.record(retryAttempt{status: 429})

	if a.Summary() != b.Summary() {
		t.Fatalf("summary depends on order: %q vs %q", a.Summary(), b.Summary())
	}
	if got, want := a.Summary(), "429x2,transportx1"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if a.Count() != 3 {
		t.Fatalf("count = %d, want 3", a.Count())
	}
}

// RateLimited isolates the hypothesis on trial (provider 429s), so a storm of
// non-429 failures must not set it.
func TestRetryCollectorRateLimitedOnlyOn429(t *testing.T) {
	c := &RetryCollector{}
	c.record(retryAttempt{status: 503})
	c.record(retryAttempt{kind: "transport"})
	if c.RateLimited() {
		t.Fatal("503/transport must not read as rate limited")
	}
	c.record(retryAttempt{status: 429})
	if !c.RateLimited() {
		t.Fatal("429 must read as rate limited")
	}
}

// Observability must never be able to change control flow: a caller that did
// not opt in gets a nil collector, and every method has to stay safe.
func TestRetryCollectorNilIsSafeAndEmpty(t *testing.T) {
	var c *RetryCollector
	c.record(retryAttempt{status: 429})
	if c.Count() != 0 || c.Summary() != "" || c.RateLimited() {
		t.Fatal("nil collector must report empty")
	}
	if retryCollectorFrom(context.Background()) != nil {
		t.Fatal("a context without a collector must yield nil")
	}
}

// A pathological retry storm must not grow the slice without bound in a
// long-lived context.
func TestRetryCollectorBoundsRetainedAttempts(t *testing.T) {
	c := &RetryCollector{}
	for i := 0; i < 500; i++ {
		c.record(retryAttempt{status: 429})
	}
	if c.Count() != 64 {
		t.Fatalf("count = %d, want the 64 cap", c.Count())
	}
}

// End to end through the real retry loop: the ledger row must show the 429s
// that were absorbed before the call finally succeeded. Without this, a turn
// that quietly burned its budget on rate limits is indistinguishable from a
// clean one.
func TestDoStreamRecordsAbsorbedRateLimitsIntoCollector(t *testing.T) {
	calls := 0
	c, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, "slow down")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	}, WithRetry(3, 10*time.Millisecond, 50*time.Millisecond))

	ctx, collector := WithRetryCollector(context.Background())
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	body := testutil.Must(c.DoStream(ctx, req))
	defer body.Close()

	if collector.Count() != 2 {
		t.Fatalf("recorded %d attempts, want 2", collector.Count())
	}
	if got, want := collector.Summary(), "429x2"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if !collector.RateLimited() {
		t.Fatal("absorbed 429s must set RateLimited")
	}
}

// A permanent failure is recorded too: "one 400" and "no failures" are very
// different rows, and the retryability check must not hide the attempt.
func TestDoStreamRecordsPermanentFailure(t *testing.T) {
	c, server := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":"bad request"}`)
	})
	ctx, collector := WithRetryCollector(context.Background())
	req, _ := http.NewRequest(http.MethodPost, server.URL+"/v1/messages", nil)
	if _, err := c.DoStream(ctx, req); err == nil {
		t.Fatal("expected error for 400")
	}
	if got, want := collector.Summary(), "400x1"; got != want {
		t.Fatalf("summary = %q, want %q", got, want)
	}
	if collector.RateLimited() {
		t.Fatal("400 is not a rate limit")
	}
}
