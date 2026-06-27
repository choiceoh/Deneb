// health_cache.go surfaces the self-hosted vLLM prefix-cache (APC) hit rate as
// an OPS signal on /health and /status — a passive regression alarm for the
// prompt-cache doctrine (.claude/rules/prompt-cache.md).
//
// The measurement is NOT new. The engine's prefix-cache counters are already
// scraped, per served model, by observe.FetchVllmPrefixCaches (the same helper
// the agent-facing `observe action=behavior` view uses). Those counters are
// cumulative since engine boot. This file reuses that helper verbatim and adds
// only aggregation: it snapshots the cumulative {queries,hits} into a small
// in-process ring and derives a 24h rolling hit ratio from the delta between
// the newest sample and the oldest sample still inside the window. No second
// scrape path, no new metric series.
//
// Graceful degradation: a host with no vLLM role configured returns no base
// URLs, so the ring stays empty and /health simply omits the "cache" section —
// it is never an error and never blocks the probe.
package server

import (
	"context"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/observe"
)

const (
	// cacheRatioWindow is the rolling window the surfaced hit ratio covers.
	cacheRatioWindow = 24 * time.Hour
	// cacheSampleMinInterval throttles re-scraping: /health may be polled every
	// few seconds by a load balancer, but the engine counters move slowly and a
	// fresh /metrics scrape per probe would be wasteful. One sample per minute is
	// plenty of resolution for a 24h ratio.
	cacheSampleMinInterval = 1 * time.Minute
	// cacheScrapeTimeout bounds the /metrics scrape kicked off by a /health hit.
	cacheScrapeTimeout = 2 * time.Second
	// cacheRingMax caps retained samples (defence in depth — at one/minute, 24h
	// is ~1440 samples; this guards against clock jumps spamming the ring).
	cacheRingMax = 4096
)

// cacheSample is one timestamped reading of the engine prefix-cache counters,
// summed across every served model on every configured vLLM base. Cumulative
// since engine boot, exactly as observe.FetchVllmPrefixCaches reports them.
type cacheSample struct {
	at      time.Time
	queries int64
	hits    int64
}

// cacheHealth holds the rolling sample ring plus the last computed one-line
// summary. It is safe for concurrent /health probes; the mutex guards both.
type cacheHealth struct {
	mu sync.Mutex
	// samples is the rolling ring of cumulative-counter readings.
	samples []cacheSample
	// lastSummary is the most recently rendered one-line status, cached so the
	// /status snapshot (which must not perform network I/O) can echo it without
	// triggering a scrape. Empty until the first /health probe with a vLLM host.
	lastSummary string
}

// cacheHealthSection is the JSON shape rendered under health["cache"]. It mirrors
// the flat, snake_case-ish style of the other /health sections.
type cacheHealthSection struct {
	// HitRatePct is the 24h rolling prefix-cache hit ratio (hits/queries*100,
	// one decimal), or nil when the window has too little data to be meaningful.
	HitRatePct *float64 `json:"hitRatePct,omitempty"`
	// WindowHits/WindowQueries are the token deltas over the covered window —
	// the numerator/denominator behind HitRatePct, exposed so an operator can
	// see whether a low ratio is real or just a thin sample.
	WindowHits    int64 `json:"windowHits"`
	WindowQueries int64 `json:"windowQueries"`
	// WindowLabel describes the actual span the numbers cover (e.g. "24h0m0s",
	// or "3m12s" right after a restart before the window fills).
	WindowLabel string `json:"window"`
	// Samples is how many readings back the ratio — 1 means only a baseline
	// exists yet (no delta, ratio omitted).
	Samples int `json:"samples"`
	// Summary is the one-line human-readable status the /status surface echoes.
	Summary string `json:"summary"`
}

// observe builds a cache health section from the current vLLM base URLs. It
// scrapes at most once per cacheSampleMinInterval (reusing the prior reading
// otherwise), appends the cumulative counters to the ring, prunes anything
// older than the window, and derives the rolling ratio. Returns ok=false when
// there is nothing to surface (no vLLM bases, scrape down with an empty ring),
// in which case /health omits the cache section entirely.
func (c *cacheHealth) observe(ctx context.Context, bases []string) (cacheHealthSection, bool) {
	now := time.Now()

	c.mu.Lock()
	needScrape := len(c.samples) == 0 || now.Sub(c.samples[len(c.samples)-1].at) >= cacheSampleMinInterval
	c.mu.Unlock()

	if needScrape && len(bases) > 0 {
		sctx, cancel := context.WithTimeout(ctx, cacheScrapeTimeout)
		queries, hits, ok := scrapeCacheCounters(sctx, bases)
		cancel()
		if ok {
			c.mu.Lock()
			c.samples = append(c.samples, cacheSample{at: now, queries: queries, hits: hits})
			if len(c.samples) > cacheRingMax {
				c.samples = c.samples[len(c.samples)-cacheRingMax:]
			}
			c.mu.Unlock()
		}
	}

	c.mu.Lock()
	pruneCacheSamples(&c.samples, now.Add(-cacheRatioWindow))
	window := append([]cacheSample(nil), c.samples...)
	c.mu.Unlock()

	sec, ok := summarizeCacheWindow(window, now)
	if ok {
		c.mu.Lock()
		c.lastSummary = sec.Summary
		c.mu.Unlock()
	}
	return sec, ok
}

// summary returns the last one-line cache status rendered by observe, or "" if
// no vLLM host has been sampled yet. Network-free: the /status snapshot reads
// this cached string instead of scraping on the status-render path.
func (c *cacheHealth) summary() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSummary
}

// scrapeCacheCounters reuses observe.FetchVllmPrefixCaches and sums the
// cumulative prefix-cache counters across every served model on every base.
// ok=false when no vLLM endpoint answered with cache series (down, or non-vLLM
// host) so the caller can leave the ring untouched.
func scrapeCacheCounters(ctx context.Context, bases []string) (queries, hits int64, ok bool) {
	rows := observe.FetchVllmPrefixCaches(ctx, bases)
	if len(rows) == 0 {
		return 0, 0, false
	}
	for _, r := range rows {
		queries += r.Queries
		hits += r.Hits
	}
	return queries, hits, true
}

// pruneCacheSamples drops samples older than cutoff but always keeps the single
// most recent pre-cutoff sample so a delta can still span the full window (the
// oldest in-window reading is the window's lower edge). Mutates *s in place.
func pruneCacheSamples(s *[]cacheSample, cutoff time.Time) {
	samples := *s
	// Index of the last sample strictly before the cutoff; keep it as the anchor.
	anchor := -1
	for i, sm := range samples {
		if sm.at.Before(cutoff) {
			anchor = i
		} else {
			break
		}
	}
	if anchor > 0 {
		*s = samples[anchor:]
	}
}

// summarizeCacheWindow turns a pruned sample ring into the rendered section.
// This is the pure aggregation seam — no I/O, no clock except the passed-in
// now — so the ratio math is unit-tested directly. The hit ratio is the delta
// between the newest and oldest retained samples (engine counters only ever
// rise between restarts; a backwards delta means the engine restarted mid
// window, so the ratio is suppressed rather than reported negative).
func summarizeCacheWindow(samples []cacheSample, now time.Time) (cacheHealthSection, bool) {
	if len(samples) == 0 {
		return cacheHealthSection{}, false
	}
	oldest := samples[0]
	newest := samples[len(samples)-1]

	sec := cacheHealthSection{
		Samples:     len(samples),
		WindowLabel: now.Sub(oldest.at).Round(time.Second).String(),
	}

	dq := newest.queries - oldest.queries
	dh := newest.hits - oldest.hits
	switch {
	case len(samples) < 2 || dq <= 0 || dh < 0:
		// Baseline only, or an engine restart reset the counters: we have a
		// reading but no trustworthy delta yet. Surface the section (so the
		// operator sees the probe is wired) but omit the ratio.
		sec.Summary = "prefix-cache: warming up (insufficient window)"
	default:
		ratio := math.Round(float64(dh)/float64(dq)*1000) / 10
		sec.HitRatePct = &ratio
		sec.WindowHits = dh
		sec.WindowQueries = dq
		sec.Summary = formatCacheSummary(ratio)
	}
	return sec, true
}

// formatCacheSummary renders the one-line /status-style cache summary. The
// wording doubles as the passive regression alarm: a hit rate that drifts low
// is legible at a glance without parsing the numeric fields.
func formatCacheSummary(ratioPct float64) string {
	state := "ok"
	switch {
	case ratioPct < 40:
		state = "LOW"
	case ratioPct < 70:
		state = "fair"
	}
	return "prefix-cache 24h hit-rate " + strconv.FormatFloat(ratioPct, 'f', 1, 64) + "% (" + state + ")"
}
