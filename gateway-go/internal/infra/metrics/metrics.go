// Package metrics provides lightweight RPC call counting for the Deneb gateway.
//
// Only the Counter type is retained — it tracks RPC call counts (method × status)
// and exposes a Snapshot for internal reporting (e.g. monitoring.rpc_zero_calls).
// Prometheus/histogram support was removed; the gateway does not use a scrape pipeline.
//
// No external dependencies — stdlib only.
package metrics

import (
	"strings"
	"sync"
	"sync/atomic"
)

// Counter is a monotonically increasing counter keyed by label values.
type Counter struct {
	mu     sync.RWMutex
	values map[string]*atomic.Int64
}

// NewCounter creates a new labeled counter.
func NewCounter() *Counter {
	return &Counter{
		values: make(map[string]*atomic.Int64),
	}
}

// Inc increments the counter for the given label values.
func (c *Counter) Inc(labelValues ...string) {
	key := strings.Join(labelValues, "\x00")
	c.mu.RLock()
	v, ok := c.values[key]
	c.mu.RUnlock()
	if ok {
		v.Add(1)
		return
	}
	c.mu.Lock()
	if v, ok = c.values[key]; ok {
		c.mu.Unlock()
		v.Add(1)
		return
	}
	v = &atomic.Int64{}
	v.Store(1)
	c.values[key] = v
	c.mu.Unlock()
}

// Snapshot returns a copy of all label-key → value pairs.
// The key is the \x00-joined label values string.
func (c *Counter) Snapshot() map[string]int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make(map[string]int64, len(c.values))
	for k, v := range c.values {
		out[k] = v.Load()
	}
	return out
}

// RPCRequestsTotal counts RPC calls keyed by "method\x00status\x00code".
// Used by monitoring.rpc_zero_calls to find never-called methods.
var RPCRequestsTotal = NewCounter()

// CacheHitTracker accumulates Anthropic-style prompt-cache token usage across
// every LLM turn in the process. It exists so /status can surface a cumulative
// cache hit ratio — the regression alarm for the prompt-cache doctrine
// (.claude/rules/prompt-cache.md): if a doctrine violation (system prompt
// rebuilt per turn, toolset churn, etc.) silently breaks caching, the ratio
// drops and the operator can see it. Safe for concurrent use.
//
// The three buckets are DISJOINT, matching Anthropic streaming usage semantics
// (see llm.MessageStart/TokenUsage): for a cached request the prompt splits
// into cache_read_input_tokens (served from cache), cache_creation_input_tokens
// (written to cache this turn), and input_tokens (the uncached/fresh remainder)
// with no overlap. Total prompt input = cacheRead + cacheCreation + freshInput,
// so summing all three is correct — do NOT pass a grand-total as freshInput.
type CacheHitTracker struct {
	cacheRead     atomic.Int64 // cache_read_input_tokens (served from cache)
	cacheCreation atomic.Int64 // cache_creation_input_tokens (written to cache)
	freshInput    atomic.Int64 // input_tokens (uncached fresh prompt — disjoint from the above)

	// recentRatio is an exponentially weighted moving average of the per-record
	// hit ratio. The cumulative buckets above answer "lifetime health", but a
	// regression alarm needs to weight RECENT runs so a fresh cache break shows
	// up even after a long healthy history (otherwise it is diluted by all the
	// earlier hits). Guarded by mu; the cumulative counters stay lock-free.
	mu          sync.Mutex
	recentRatio float64
	recentSeen  bool
}

// recentEWMAAlpha controls how fast RecentRatio forgets older runs. 0.1 means
// roughly the last ~10 runs dominate the estimate — recent enough to surface a
// regression, smooth enough not to swing on a single outlier run.
const recentEWMAAlpha = 0.1

// Record adds the token counts from one completed LLM run (or turn).
// freshInput must be the uncached remainder (Anthropic usage.input_tokens),
// which is disjoint from cacheRead/cacheCreation — not a grand-total.
// Non-positive values are ignored. When the record carries any prompt tokens,
// its hit ratio is folded into the recent EWMA.
func (c *CacheHitTracker) Record(cacheRead, cacheCreation, freshInput int64) {
	if cacheRead > 0 {
		c.cacheRead.Add(cacheRead)
	}
	if cacheCreation > 0 {
		c.cacheCreation.Add(cacheCreation)
	}
	if freshInput > 0 {
		c.freshInput.Add(freshInput)
	}

	if total := cacheRead + cacheCreation + freshInput; total > 0 {
		r := float64(cacheRead) / float64(total)
		c.mu.Lock()
		if c.recentSeen {
			c.recentRatio = recentEWMAAlpha*r + (1-recentEWMAAlpha)*c.recentRatio
		} else {
			c.recentRatio = r
			c.recentSeen = true
		}
		c.mu.Unlock()
	}
}

// RecentRatio returns the EWMA of recent per-record hit ratios, emphasizing the
// most recent runs so a fresh cache regression is visible even after a long
// healthy history. The bool is false until at least one record carries prompt
// tokens.
func (c *CacheHitTracker) RecentRatio() (float64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recentRatio, c.recentSeen
}

// Snapshot returns cumulative (cacheRead, cacheCreation, freshInput) totals.
func (c *CacheHitTracker) Snapshot() (cacheRead, cacheCreation, freshInput int64) {
	return c.cacheRead.Load(), c.cacheCreation.Load(), c.freshInput.Load()
}

// HitRatioOf computes the cache hit fraction from an already-read snapshot:
// cacheRead / (cacheRead + cacheCreation + freshInput). Returns 0 when the
// total is zero. Callers that display the underlying counts should use this on
// their snapshot so the shown ratio and counts stay consistent (rather than
// re-loading the atomics, which can drift under concurrent updates).
func HitRatioOf(cacheRead, cacheCreation, freshInput int64) float64 {
	total := cacheRead + cacheCreation + freshInput
	if total == 0 {
		return 0
	}
	return float64(cacheRead) / float64(total)
}

// HitRatio returns the fraction of total prompt input tokens served from
// cache. Returns 0 when no prompt tokens have been recorded yet.
func (c *CacheHitTracker) HitRatio() float64 {
	return HitRatioOf(c.Snapshot())
}

// CacheHits is the process-wide prompt-cache usage tracker, read by /status.
var CacheHits CacheHitTracker
