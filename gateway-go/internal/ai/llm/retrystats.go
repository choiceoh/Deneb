package llm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Retry observability, context-scoped.
//
// Why a context sideband rather than a return value: the retry loop lives in
// DoStream, three layers under the agent turn, and is shared by helper calls
// that have no run log at all. Threading a stats struct back up would change
// the streaming signature for every caller to serve one observer. This mirrors
// pkg/toolmeta, which carries per-tool metadata the same way.
//
// What it buys: on 2026-07-25 the production ledger showed 6.6% of real user
// turns timing out, with k3's p95 pinned at exactly the 300s ceiling — but
// turn.llm recorded no retry, status, or fallback field, so "is the provider
// rate-limiting us?" could not be answered from the ledger at all, and the
// #4215 mitigation (surface rate limits early so the fallback chain can switch)
// had no way to be verified. Attempts are recorded here; the agent runner
// stamps them onto the turn.

// retryAttempt is one failed attempt within a single DoStream call.
type retryAttempt struct {
	// status is the HTTP status when the provider answered (429, 503, …), or 0
	// when the request never completed (transport error, timeout).
	status int
	// kind labels a status-less failure so a transport storm is not silently
	// indistinguishable from a rate-limit storm.
	kind string
}

// RetryCollector accumulates failed attempts for one logical LLM call. Safe for
// concurrent use: a streaming turn and its watchdog can touch it from separate
// goroutines.
type RetryCollector struct {
	mu       sync.Mutex
	attempts []retryAttempt
}

type retryCollectorKey struct{}

// WithRetryCollector returns a context carrying a fresh collector. Callers that
// want per-turn granularity must call this once per turn — a collector reused
// across turns reports the run's total, not the turn's.
func WithRetryCollector(ctx context.Context) (context.Context, *RetryCollector) {
	c := &RetryCollector{}
	return context.WithValue(ctx, retryCollectorKey{}, c), c
}

// retryCollectorFrom returns the collector on ctx, or nil. A nil collector makes
// every record a no-op, so instrumentation never depends on the caller having
// opted in.
func retryCollectorFrom(ctx context.Context) *RetryCollector {
	c, _ := ctx.Value(retryCollectorKey{}).(*RetryCollector)
	return c
}

func (c *RetryCollector) record(a retryAttempt) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Bound the slice: a pathological retry storm must not grow unboundedly in
	// a long-lived context. The summary only needs counts, and the cap is far
	// above any configured maxRetries.
	if len(c.attempts) >= 64 {
		return
	}
	c.attempts = append(c.attempts, a)
}

// Count returns how many attempts failed and were retried or surfaced.
func (c *RetryCollector) Count() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.attempts)
}

// Summary renders the failure mix as a stable, compact histogram — "429x3" or
// "429x2,transport x1" — deterministically ordered so the same mix always
// produces the same string (ledger rows are grouped by it downstream).
// Empty when nothing failed.
func (c *RetryCollector) Summary() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.attempts) == 0 {
		return ""
	}
	counts := make(map[string]int, len(c.attempts))
	for _, a := range c.attempts {
		label := a.kind
		if a.status > 0 {
			label = fmt.Sprintf("%d", a.status)
		}
		if label == "" {
			label = "unknown"
		}
		counts[label]++
	}
	labels := make([]string, 0, len(counts))
	for label := range counts {
		labels = append(labels, label)
	}
	sort.Strings(labels)
	parts := make([]string, 0, len(labels))
	for _, label := range labels {
		parts = append(parts, fmt.Sprintf("%sx%d", label, counts[label]))
	}
	return strings.Join(parts, ",")
}

// RateLimited reports whether any attempt was a provider rate limit (429). This
// is the specific question the timeout investigation needs answered per turn.
func (c *RetryCollector) RateLimited() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, a := range c.attempts {
		if a.status == 429 {
			return true
		}
	}
	return false
}
