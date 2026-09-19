package health

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
	"github.com/choiceoh/deneb/gateway-go/internal/core/observe"
)

// measuredDay is one engine model's day whose token-reuse counters were
// published at both ends of its intervals.
func measuredDay(day, endpoint, model string, cached, prompt float64, polls int) enginespeed.DayStat {
	return enginespeed.DayStat{
		Day: day, Endpoint: endpoint, Model: model, Polls: polls,
		Delta: observe.EngineDelta{PromptTokens: prompt, CachePromptTokens: prompt, CachedPromptTokens: cached},
	}
}

func TestCacheFromEngineDaysSumsTheNewestDayAcrossEngines(t *testing.T) {
	days := []enginespeed.DayStat{ // newest first, as Store.Days returns them
		measuredDay("2026-09-19", "http://a:8000/metrics", "glm-5.3-flash", 1_840_128, 4_161_535, 3393),
		measuredDay("2026-09-19", "http://a:8000/metrics", "qwen3.8-flash-next", 60_000, 100_000, 138),
		measuredDay("2026-09-18", "http://a:8000/metrics", "glm-5.3-flash", 9_000_000, 9_000_000, 3486),
	}
	sec, ok := CacheFromEngineDays(days)
	if !ok || sec.HitRatePct == nil {
		t.Fatalf("section = (%+v, %v), want a ratio", sec, ok)
	}
	// (1,840,128 + 60,000) / (4,161,535 + 100,000) = 44.6% — yesterday's
	// perfect day must not leak into today's alarm.
	if *sec.HitRatePct != 44.6 {
		t.Errorf("HitRatePct = %v, want 44.6", *sec.HitRatePct)
	}
	if sec.WindowHits != 1_900_128 || sec.WindowQueries != 4_261_535 {
		t.Errorf("window = (hits %d, queries %d), want the newest day's token sums", sec.WindowHits, sec.WindowQueries)
	}
	if sec.WindowLabel != "2026-09-19" || sec.Samples != 3393+138 {
		t.Errorf("label/samples = %q/%d", sec.WindowLabel, sec.Samples)
	}
	if !strings.Contains(sec.Summary, "token reuse 44.6% (fair)") {
		t.Errorf("Summary = %q", sec.Summary)
	}
}

// ST counts its vllm:prefix_cache_* series in REQUESTS. A day that has only
// those — no token-reuse interval — must read as unmeasured, not as a ratio of
// requests (8 of 130 would have alarmed LOW at 6.2% while the engine reused
// 59% of prompt tokens).
func TestCacheFromEngineDaysNeverReadsRequestCountsAsTokens(t *testing.T) {
	days := []enginespeed.DayStat{{
		Day: "2026-09-19", Endpoint: "http://a:8000/metrics", Polls: 12,
		Delta: observe.EngineDelta{PromptTokens: 423_319, PrefixCacheQueries: 130, PrefixCacheHits: 8},
	}}
	sec, ok := CacheFromEngineDays(days)
	if !ok {
		t.Fatal("a configured engine with history must keep its section")
	}
	if sec.HitRatePct != nil || sec.WindowQueries != 0 {
		t.Fatalf("request counts leaked into the token ratio: %+v", sec)
	}
	if !strings.Contains(sec.Summary, "not measured") {
		t.Errorf("Summary = %q, want it to say the day is unmeasured", sec.Summary)
	}
}

// An unmeasured interval on the same day stays out of the denominator instead
// of diluting the measured ones toward zero.
func TestCacheFromEngineDaysLeavesUnmeasuredRowsOutOfTheDenominator(t *testing.T) {
	days := []enginespeed.DayStat{
		measuredDay("2026-09-19", "http://a:8000/metrics", "glm-5.3-flash", 900, 1000, 10),
		{Day: "2026-09-19", Endpoint: "http://b:8000/metrics", Polls: 10, Delta: observe.EngineDelta{PromptTokens: 50_000}},
	}
	sec, ok := CacheFromEngineDays(days)
	if !ok || sec.HitRatePct == nil || *sec.HitRatePct != 90 {
		t.Fatalf("section = %+v, want 90%% from the measured row alone", sec)
	}
	if sec.Samples != 20 {
		t.Errorf("Samples = %d, want both rows' polls", sec.Samples)
	}
}

func TestCacheFromEngineDaysOmitsTheSectionWithoutHistory(t *testing.T) {
	if _, ok := CacheFromEngineDays(nil); ok {
		t.Fatal("no engine history must omit the section")
	}
}

func TestCacheFromEngineDaysRoundsAndLabels(t *testing.T) {
	cases := []struct {
		cached, prompt float64
		pct            float64
		label          string
	}{
		{300_000, 700_000, 42.9, "(fair)"},
		{950_000, 1_000_000, 95, "(ok)"},
		{100_000, 1_000_000, 10, "(LOW)"},
		// A day a turn or two deep reports its ratio but no verdict.
		{0, 14, 0, "(thin sample: 14 prompt tokens)"},
	}
	for _, c := range cases {
		sec, ok := CacheFromEngineDays([]enginespeed.DayStat{measuredDay("2026-09-19", "e", "m", c.cached, c.prompt, 1)})
		if !ok || sec.HitRatePct == nil || *sec.HitRatePct != c.pct || !strings.Contains(sec.Summary, c.label) {
			t.Errorf("%v/%v: section = %+v, want %.1f%% %s", c.cached, c.prompt, sec, c.pct, c.label)
		}
	}
}

// /health hands Collect a getter; a nil one (no engine configured) omits the
// cache section while the GPU half still runs.
func TestProbesCollectReadsTheEngineHistoryItIsHanded(t *testing.T) {
	var p Probes
	// A fresh cached GPU reading, so the GPU half never execs nvidia-smi here.
	p.gpu.probed, p.gpu.cachedAt = true, time.Now()
	if got := p.Collect(context.Background(), nil); got.CachePresent {
		t.Fatalf("nil history getter produced a cache section: %+v", got.Cache)
	}
	got := p.Collect(context.Background(), func() []enginespeed.DayStat {
		return []enginespeed.DayStat{measuredDay("2026-09-19", "e", "m", 500, 1000, 4)}
	})
	if !got.CachePresent || got.Cache.HitRatePct == nil || *got.Cache.HitRatePct != 50 {
		t.Fatalf("cache section = %+v (present=%v)", got.Cache, got.CachePresent)
	}
}

// The thin-sample rule withholds only the verdict: the ratio and its mass are
// still there for a reader who wants them.
func TestCacheFromEngineDaysGivesNoVerdictOnAThinDay(t *testing.T) {
	sec, ok := CacheFromEngineDays([]enginespeed.DayStat{measuredDay("2026-09-19", "e", "m", 0, 14, 11)})
	if !ok || sec.HitRatePct == nil || *sec.HitRatePct != 0 || sec.WindowQueries != 14 {
		t.Fatalf("section = %+v", sec)
	}
	for _, verdict := range []string{"(LOW)", "(fair)", "(ok)"} {
		if strings.Contains(sec.Summary, verdict) {
			t.Errorf("a 14-token day gave the verdict %s: %q", verdict, sec.Summary)
		}
	}
}
