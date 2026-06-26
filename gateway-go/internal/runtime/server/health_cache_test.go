package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

func TestSummarizeCacheWindow(t *testing.T) {
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	mk := func(ago time.Duration, q, h int64) cacheSample {
		return cacheSample{at: now.Add(-ago), queries: q, hits: h}
	}

	t.Run("empty ring omits section", func(t *testing.T) {
		_, ok := summarizeCacheWindow(nil, now)
		if ok {
			t.Errorf("ok = true for empty ring, want false (section omitted)")
		}
	})

	t.Run("single sample is baseline only, ratio omitted", func(t *testing.T) {
		sec, ok := summarizeCacheWindow([]cacheSample{mk(time.Minute, 1000, 800)}, now)
		if !ok {
			t.Fatalf("ok = false for baseline sample, want true (section present)")
		}
		if sec.HitRatePct != nil {
			t.Errorf("HitRatePct = %v for baseline, want nil", *sec.HitRatePct)
		}
		if sec.Samples != 1 {
			t.Errorf("Samples = %d, want 1", sec.Samples)
		}
	})

	t.Run("rolling ratio is delta between oldest and newest", func(t *testing.T) {
		// Oldest 24h ago: 1000 queries / 700 hits. Newest now: 3000 / 2500.
		// Delta: 2000 queries, 1800 hits → 90.0%.
		samples := []cacheSample{
			mk(24*time.Hour, 1000, 700),
			mk(12*time.Hour, 2000, 1500),
			mk(0, 3000, 2500),
		}
		sec, ok := summarizeCacheWindow(samples, now)
		if !ok || sec.HitRatePct == nil {
			t.Fatalf("summarize = (%+v, %v), want a ratio", sec, ok)
		}
		if *sec.HitRatePct != 90.0 {
			t.Errorf("HitRatePct = %v, want 90.0", *sec.HitRatePct)
		}
		if sec.WindowQueries != 2000 || sec.WindowHits != 1800 {
			t.Errorf("window deltas = (q=%d, h=%d), want (2000, 1800)", sec.WindowQueries, sec.WindowHits)
		}
		if sec.Samples != 3 {
			t.Errorf("Samples = %d, want 3", sec.Samples)
		}
	})

	t.Run("ratio rounds to one decimal", func(t *testing.T) {
		// Delta 3 hits / 7 queries = 42.857% → 42.9.
		samples := []cacheSample{mk(time.Hour, 0, 0), mk(0, 7, 3)}
		sec, ok := summarizeCacheWindow(samples, now)
		if !ok || sec.HitRatePct == nil || *sec.HitRatePct != 42.9 {
			t.Errorf("HitRatePct = %v (ok=%v), want 42.9", sec.HitRatePct, ok)
		}
	})

	t.Run("engine restart (counter went backwards) suppresses ratio", func(t *testing.T) {
		// Newest has lower cumulative counters than oldest → restart; no negative
		// ratio, surface the section warming-up instead.
		samples := []cacheSample{mk(2*time.Hour, 5000, 4000), mk(0, 100, 80)}
		sec, ok := summarizeCacheWindow(samples, now)
		if !ok {
			t.Fatalf("ok = false after restart, want true (section present, warming up)")
		}
		if sec.HitRatePct != nil {
			t.Errorf("HitRatePct = %v after restart, want nil", *sec.HitRatePct)
		}
	})

	t.Run("zero query delta (idle engine) suppresses ratio", func(t *testing.T) {
		samples := []cacheSample{mk(time.Hour, 1000, 900), mk(0, 1000, 900)}
		sec, ok := summarizeCacheWindow(samples, now)
		if !ok || sec.HitRatePct != nil {
			t.Errorf("idle engine: section=(%+v, %v), want present with nil ratio", sec, ok)
		}
	})
}

func TestSummarizeCacheWindowSummaryStates(t *testing.T) {
	now := time.Now()
	mk := func(q, h int64) []cacheSample {
		return []cacheSample{{at: now.Add(-time.Hour), queries: 0, hits: 0}, {at: now, queries: q, hits: h}}
	}
	cases := []struct {
		desc     string
		q, h     int64
		contains string
	}{
		{"high hit-rate is ok", 1000, 950, "(ok)"},
		{"mid hit-rate is fair", 1000, 550, "(fair)"},
		{"low hit-rate flags LOW", 1000, 100, "(LOW)"},
	}
	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			sec, ok := summarizeCacheWindow(mk(tc.q, tc.h), now)
			if !ok {
				t.Fatalf("ok = false")
			}
			if !strings.Contains(sec.Summary, tc.contains) {
				t.Errorf("Summary = %q, want to contain %q", sec.Summary, tc.contains)
			}
		})
	}
}

func TestPruneCacheSamples(t *testing.T) {
	base := time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC)
	at := func(h int) time.Time { return base.Add(time.Duration(h) * time.Hour) }

	t.Run("keeps one pre-cutoff anchor so delta spans full window", func(t *testing.T) {
		samples := []cacheSample{
			{at: at(0)}, {at: at(1)}, {at: at(10)}, {at: at(20)},
		}
		// Cutoff at hour 5: hours 0 and 1 are pre-cutoff; only the latest pre-cutoff
		// (hour 1) is kept as the anchor.
		pruneCacheSamples(&samples, at(5))
		if len(samples) != 3 {
			t.Fatalf("len = %d, want 3 (anchor + 2 in-window)", len(samples))
		}
		if !samples[0].at.Equal(at(1)) {
			t.Errorf("anchor = %v, want hour 1 (latest pre-cutoff)", samples[0].at)
		}
	})

	t.Run("no pruning when all in window", func(t *testing.T) {
		samples := []cacheSample{{at: at(6)}, {at: at(7)}}
		pruneCacheSamples(&samples, at(5))
		if len(samples) != 2 {
			t.Errorf("len = %d, want 2 (nothing pruned)", len(samples))
		}
	})

	t.Run("single pre-cutoff sample retained as sole anchor", func(t *testing.T) {
		samples := []cacheSample{{at: at(0)}}
		pruneCacheSamples(&samples, at(5))
		if len(samples) != 1 {
			t.Errorf("len = %d, want 1 (anchor retained)", len(samples))
		}
	})
}

// TestCacheHealthObserveNoBasesDegrades verifies that with no vLLM bases the
// collector never scrapes, the ring stays empty, and observe reports nothing to
// surface — so /health omits the cache section on non-vLLM hosts.
func TestCacheHealthObserveNoBasesDegrades(t *testing.T) {
	var c cacheHealth
	_, ok := c.observe(context.Background(), nil)
	if ok {
		t.Errorf("ok = true with no vLLM bases, want false (cache section omitted)")
	}
	if s := c.summary(); s != "" {
		t.Errorf("summary = %q with no bases, want empty", s)
	}
}

// TestStatusReportEchoesCacheSummary verifies the /status surface: the status
// snapshot appends the cache one-liner when the accessor returns one, and omits
// it cleanly when the accessor is unset (non-vLLM host).
func TestStatusReportEchoesCacheSummary(t *testing.T) {
	t.Run("appends cache line when summary present", func(t *testing.T) {
		n := &notifyService{
			sessions:     session.NewManager(),
			cacheSummary: func() string { return "prefix-cache 24h hit-rate 91.2% (ok)" },
		}
		report := n.buildStatusReport(time.Now())
		if !strings.Contains(report, "prefix-cache 24h hit-rate 91.2% (ok)") {
			t.Errorf("status report missing cache line, got:\n%s", report)
		}
	})

	t.Run("omits cache line when accessor unset", func(t *testing.T) {
		n := &notifyService{sessions: session.NewManager()}
		report := n.buildStatusReport(time.Now())
		if strings.Contains(report, "prefix-cache") {
			t.Errorf("status report should not mention cache when unset, got:\n%s", report)
		}
	})

	t.Run("omits cache line when accessor returns empty", func(t *testing.T) {
		n := &notifyService{
			sessions:     session.NewManager(),
			cacheSummary: func() string { return "" },
		}
		report := n.buildStatusReport(time.Now())
		if strings.Contains(report, "prefix-cache") {
			t.Errorf("status report should not mention cache when summary empty, got:\n%s", report)
		}
	})
}
