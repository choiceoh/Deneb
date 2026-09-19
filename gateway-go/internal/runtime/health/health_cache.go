// health_cache.go surfaces the local serving engines' prompt-cache reuse as an
// OPS signal on /health — a passive regression alarm for the prompt-cache
// doctrine (docs/agent-rules/prompt-cache.md).
//
// Nothing is measured here. The engine-speed task (internal/ai/enginespeed)
// already scrapes every configured engine (DENEB_ENGINE_METRICS_URL) on a fixed
// cadence and folds each interval into its local day, in prompt TOKENS: ST's
// st:prefix_reused_tokens_total against vllm:prompt_tokens_total, never ST's
// vllm:prefix_cache_* counters, which count REQUESTS. This file only sums the
// newest day's rows into one ratio.
//
// It used to keep its own 24h scrape ring over the model registry's
// vllm-provider base URLs. Every role has gone through the router since the
// wormhole cutover (2026-06-14), so that list was empty and /health carried no
// cache section at all — and had it reached the ST engine, it would have read
// request counts as tokens.
//
// Graceful degradation: no engine configured (no history) → no section. A
// newest day with no measured interval → the section without a ratio.
package health

import (
	"math"
	"strconv"

	"github.com/choiceoh/deneb/gateway-go/internal/ai/enginespeed"
)

// cacheVerdictMinTokens is the prompt-token mass below which the summary gives
// no ok/fair/LOW verdict. Minutes after midnight the day is a turn or two deep
// (one prompt head alone is ~40K tokens), and a ratio from that is a coin toss,
// not an alarm: a dev gateway's history once read "0.0% (LOW)" off 14 tokens.
const cacheVerdictMinTokens = 100_000

// CacheSection is the JSON shape rendered under health["cache"]. It mirrors
// the flat, snake_case-ish style of the other /health sections.
type CacheSection struct {
	// HitRatePct is the share of prompt tokens the engines already had
	// resident on the covered day (one decimal), or nil when that day has no
	// measured interval yet.
	HitRatePct *float64 `json:"hitRatePct,omitempty"`
	// WindowHits/WindowQueries are the reused and looked-up prompt tokens
	// behind HitRatePct, exposed so an operator can see whether a low ratio is
	// real or just a thin sample (the day has barely started).
	WindowHits    int64 `json:"windowHits"`
	WindowQueries int64 `json:"windowQueries"`
	// WindowLabel is the covered day (YYYY-MM-DD, local).
	WindowLabel string `json:"window"`
	// Samples is how many engine scrapes the day folds in.
	Samples int `json:"samples"`
	// Summary is the one-line human-readable status.
	Summary string `json:"summary"`
}

// CacheFromEngineDays summarizes the newest day of the engine-speed history
// (enginespeed.Store.Days, newest first) across every engine and model measured
// on it. ok=false when there is no history, so /health omits the section.
func CacheFromEngineDays(days []enginespeed.DayStat) (CacheSection, bool) {
	if len(days) == 0 {
		return CacheSection{}, false
	}
	day := days[0].Day
	sec := CacheSection{WindowLabel: day}
	for _, d := range days {
		if d.Day != day {
			break
		}
		sec.Samples += d.Polls
		// Only intervals whose both ends carried token-reuse counters count;
		// the rest of the day stays out of the denominator rather than
		// diluting the ratio toward zero.
		if r := d.Rates(); r.PromptCacheMeasured {
			sec.WindowHits += r.CachedPromptTokens
			sec.WindowQueries += r.CachePromptTokens
		}
	}
	if sec.WindowQueries <= 0 {
		sec.Summary = "prefix-cache " + day + ": not measured (no token-reuse interval yet)"
		return sec, true
	}
	ratio := math.Round(float64(sec.WindowHits)/float64(sec.WindowQueries)*1000) / 10
	sec.HitRatePct = &ratio
	sec.Summary = formatCacheSummary(day, ratio, sec.WindowQueries)
	return sec, true
}

// formatCacheSummary renders the one-line cache summary. The wording doubles as
// the passive regression alarm: a reuse ratio that drifts low is legible at a
// glance without parsing the numeric fields — once the day holds enough prompt
// tokens for the ratio to mean something.
func formatCacheSummary(day string, ratioPct float64, promptTokens int64) string {
	head := "prefix-cache " + day + " token reuse " + strconv.FormatFloat(ratioPct, 'f', 1, 64) + "% "
	if promptTokens < cacheVerdictMinTokens {
		return head + "(thin sample: " + strconv.FormatInt(promptTokens, 10) + " prompt tokens)"
	}
	state := "ok"
	switch {
	case ratioPct < 40:
		state = "LOW"
	case ratioPct < 70:
		state = "fair"
	}
	return head + "(" + state + ")"
}
