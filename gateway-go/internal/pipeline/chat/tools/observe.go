package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/domain/workfeed"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/observe"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

// proactiveStaleWindowMs is how long a delivered proactive card may sit unread
// before it counts as ignored (over-intervention). 48h: a work-feed card the
// user has not touched in two days was, in effect, an interruption without value.
const proactiveStaleWindowMs = 48 * 60 * 60 * 1000

// ToolObserve lets the agent inspect its OWN runtime through the in-process
// observation plane (internal/runtime/observe) — the same core the external
// `deneb observe` CLI and miniapp.observe.* RPC read. Three actions:
//
//   - turn:     one run's shape (tokens/tools/cache/compaction) joined with its
//     captured log lines — "what happened on run X, and why"
//   - logs:     query the in-memory log ring (runId/session/level/contains)
//   - behavior: cross-session roll-up (tool usage, proactive funnel, bg jobs),
//     plus the vLLM engine's prefix-cache hit rate scraped live from /metrics
//
// vllmBases lazily lists the OpenAI-mode vLLM role base URLs to scrape for
// engine-level prefix-cache counters (nil or empty → the line is omitted).
// Some vLLM builds never fill per-request cached_tokens, so this scrape is
// the only reliable cache signal there.
//
// This is the self-observation adapter: the self-evolution loop or the operator
// in chat ("방금 그 턴 왜 느렸어?") can read it without leaving the agent.
func ToolObserve(lc *observe.LogCapture, alog *agentlog.Writer, wf *workfeed.Store, vllmBases func() []string) ToolFunc {
	return func(ctx context.Context, input json.RawMessage) (string, error) {
		var p struct {
			Action   string  `json:"action"`
			RunID    string  `json:"runId"`
			Session  string  `json:"session"`
			Level    string  `json:"level"`
			Days     flexInt `json:"days"`
			Limit    flexInt `json:"limit"`
			Contains string  `json:"contains"`
		}
		if err := jsonutil.UnmarshalInto("observe params", input, &p); err != nil {
			return "", err
		}

		var ring *observe.Ring
		if lc != nil {
			ring = lc.Ring()
		}

		switch p.Action {
		case "turn":
			if p.RunID == "" {
				return "", fmt.Errorf("observe turn requires runId")
			}
			return formatObserveTurn(observe.BuildTurnView(alog, ring, p.RunID)), nil

		case "logs":
			if ring == nil {
				return "log capture is not wired (no recent log ring available)", nil
			}
			lines := ring.Query(observe.QueryOpts{
				RunID:    p.RunID,
				Session:  p.Session,
				MinLevel: observe.ParseLevel(p.Level),
				Contains: p.Contains,
				Limit:    p.Limit.Int(),
			})
			return formatObserveLogs(lines), nil

		case "behavior":
			if alog == nil {
				return "agent log is not wired (no behavior data available)", nil
			}
			var since int64
			if days := p.Days.Int(); days > 0 {
				since = time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
			}
			out := formatObserveBehavior(alog.Aggregate(since), p.Days.Int())
			if vllmBases != nil {
				out += formatVllmPrefixCaches(observe.FetchVllmPrefixCaches(ctx, vllmBases()))
			}
			return out, nil

		case "effort":
			if alog == nil {
				return "agent log is not wired (no effort-router data available)", nil
			}
			var since int64
			if days := p.Days.Int(); days > 0 {
				since = time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
			}
			return formatObserveEffort(alog.AggregateEffort(since), p.Days.Int()), nil

		case "proactive":
			if wf == nil {
				return "work feed is not wired (no proactive engagement data available)", nil
			}
			stat, err := wf.Engagement(time.Now().UnixMilli(), proactiveStaleWindowMs)
			if err != nil {
				return "", fmt.Errorf("observe proactive: %w", err)
			}
			return formatObserveProactive(stat), nil

		default:
			return "", fmt.Errorf("observe: unknown action %q — use turn | logs | behavior | effort | proactive", p.Action)
		}
	}
}

func formatObserveTurn(v observe.TurnView) string {
	var b strings.Builder
	fmt.Fprintf(&b, "run %s — session=%s found=%v\n", v.RunID, v.Session, v.Found)
	if v.Start != nil {
		fmt.Fprintf(&b, "  start: model=%s provider=%s\n", v.Start.Model, v.Start.Provider)
	}
	if v.End != nil {
		fmt.Fprintf(&b, "  end: stop=%s turns=%d in=%d out=%d cacheRead=%d compacted=%v proactive=%v toolCalls=%d %dms\n",
			v.End.StopReason, v.End.Turns, v.End.InputTokens, v.End.OutputTokens,
			v.End.CacheReadTokens, v.End.Compacted, v.End.Proactive, v.End.ToolCalls, v.End.TotalMs)
	}
	if v.Failure != nil {
		fmt.Fprintf(&b, "  ERROR: %s (aborted=%v)\n", v.Failure.Error, v.Failure.Aborted)
	}
	if eff := formatTurnEffort(v.Turns); eff != "" {
		b.WriteString(eff)
	}
	if len(v.Tools) > 0 {
		b.WriteString("  tools:\n")
		for _, t := range v.Tools {
			suffix := ""
			if t.IsError {
				suffix = " — ERROR: " + t.Error
			}
			fmt.Fprintf(&b, "    %s %dms (out %dB)%s\n", t.Name, t.DurationMs, t.OutputLen, suffix)
		}
	}
	if len(v.Logs) > 0 {
		fmt.Fprintf(&b, "  logs (%d, newest first):\n", len(v.Logs))
		for _, l := range v.Logs {
			fmt.Fprintf(&b, "    [%s] %s\n", l.Level, l.Msg)
		}
	} else if v.Found {
		b.WriteString("  logs: none in the ring (older run — rotated out)\n")
	}
	return b.String()
}

func formatObserveLogs(lines []observe.LogLine) string {
	if len(lines) == 0 {
		return "no matching log lines in the recent ring"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d log lines (newest first):\n", len(lines))
	for _, l := range lines {
		tag := ""
		if l.RunID != "" {
			tag = " run=" + l.RunID
		} else if l.Session != "" {
			tag = " sess=" + l.Session
		}
		fmt.Fprintf(&b, "  [%s]%s %s\n", l.Level, tag, l.Msg)
	}
	return b.String()
}

// formatTurnEffort renders the per-step effort decisions across a run's turns —
// thinking on/off and the observation size that informed each (the per-step
// label feed for a future learned router). Empty when the effort router was
// inactive (no turn carried a routed decision or observation).
func formatTurnEffort(turns []agentlog.TurnLLMData) string {
	parts := make([]string, 0, len(turns))
	hasSignal := false
	for _, t := range turns {
		mode := "on"
		if t.ThinkingOff {
			mode = "off"
			hasSignal = true
		}
		if t.ObsRunes > 0 {
			hasSignal = true
		}
		parts = append(parts, fmt.Sprintf("t%d:%s/obs=%d", t.Turn, mode, t.ObsRunes))
	}
	if !hasSignal {
		return ""
	}
	return "  effort: " + strings.Join(parts, " ") + "\n"
}

func formatObserveBehavior(agg agentlog.AggregateResult, days int) string {
	var b strings.Builder
	window := "all retained history"
	if days > 0 {
		window = fmt.Sprintf("last %dd", days)
	}
	fmt.Fprintf(&b, "behavior (%s): runs=%d proactive=%d compacted=%d in=%d out=%d cacheRead=%d\n",
		window, agg.Runs, agg.ProactiveRuns, agg.CompactedRuns,
		agg.TotalInputTokens, agg.TotalOutputTokens, agg.CacheReadTokens)
	if len(agg.Tools) > 0 {
		b.WriteString("  tools (by calls):\n")
		for i, t := range agg.Tools {
			if i >= 15 {
				fmt.Fprintf(&b, "    … and %d more tools\n", len(agg.Tools)-15)
				break
			}
			fmt.Fprintf(&b, "    %s: %d calls, %d err, %dms avg\n", t.Name, t.Calls, t.Errors, t.AvgMs)
		}
	}
	if len(agg.ProactiveDecisions) > 0 {
		fmt.Fprintf(&b, "  proactive funnel: %v\n", agg.ProactiveDecisions)
	}
	if len(agg.BackgroundErrors) > 0 {
		fmt.Fprintf(&b, "  background errors: %v\n", agg.BackgroundErrors)
	}
	return b.String()
}

// formatObserveEffort renders the adaptive effort-router scorecard: how often
// thinking was routed off vs kept, the escalation rate (routed-off runs that
// had to restore thinking — the recalibration signal), which gates kept it on,
// and the rough output-token savings. Empty when the router has not run in the
// window (gate closed or no traffic).
func formatObserveEffort(s agentlog.EffortStat, days int) string {
	window := "all retained history"
	if days > 0 {
		window = fmt.Sprintf("last %dd", days)
	}
	total := s.RoutedRuns + s.KeptRuns
	if total == 0 {
		return fmt.Sprintf("effort router (%s): no router-active runs — gate closed (non-dual-mode model or DENEB_ADAPTIVE_EFFORT off) or no traffic.\n", window)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "effort router (%s): active=%d routed-off=%d (%.0f%%) kept-on=%d\n",
		window, total, s.RoutedRuns, s.RoutedShare()*100, s.KeptRuns)
	fmt.Fprintf(&b, "  escalation: %d/%d routed-off runs restored thinking (%.0f%%)",
		s.EscalatedRuns, s.RoutedRuns, s.EscalationRate()*100)
	switch {
	case s.RoutedRuns == 0:
		b.WriteString(" — router never routed off; gates may be too strict\n")
	case s.EscalationRate() >= 0.15:
		b.WriteString(" — high: gates may be too aggressive (routing off when thinking was needed)\n")
	default:
		b.WriteString(" — healthy\n")
	}
	if s.RoutedTimeout > 0 {
		fmt.Fprintf(&b, "  routed-off outcomes: %d clean, %d timeout\n", s.RoutedEndTurn, s.RoutedTimeout)
	}
	if avgR, avgK := meanTokens(s.RoutedOutputTokens, s.RoutedRuns), meanTokens(s.KeptOutputTokens, s.KeptRuns); avgK > 0 {
		fmt.Fprintf(&b, "  mean output tokens: routed-off=%d kept-on=%d (savings proxy)\n", avgR, avgK)
	}
	if len(s.KeptReasons) > 0 {
		reasons := make([]string, 0, len(s.KeptReasons))
		for r := range s.KeptReasons {
			reasons = append(reasons, r)
		}
		sort.Slice(reasons, func(i, j int) bool {
			if s.KeptReasons[reasons[i]] != s.KeptReasons[reasons[j]] {
				return s.KeptReasons[reasons[i]] > s.KeptReasons[reasons[j]]
			}
			return reasons[i] < reasons[j]
		})
		b.WriteString("  kept-on gates: ")
		parts := make([]string, len(reasons))
		for i, r := range reasons {
			parts[i] = fmt.Sprintf("%s=%d", r, s.KeptReasons[r])
		}
		b.WriteString(strings.Join(parts, " ") + "\n")
	}
	return b.String()
}

func meanTokens(sum int64, n int) int64 {
	if n == 0 {
		return 0
	}
	return sum / int64(n)
}

// formatObserveProactive renders the proactive-card engagement scorecard: of the
// retained delivered cards, how many the user engaged (acked/snoozed) vs ignored
// (unread past 48h), the resulting FTR (over-intervention rate), and which
// sources are over-firing. The companion to action=behavior's delivery funnel —
// behavior shows what was delivered, this shows whether it was worth delivering.
func formatObserveProactive(s workfeed.EngagementStat) string {
	if s.Total == 0 {
		return "proactive engagement: no retained cards.\n"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "proactive engagement (retained cards): total=%d engaged=%d ignored=%d pending=%d\n",
		s.Total, s.Engaged, s.Ignored, s.Pending)
	if s.Engaged+s.Ignored > 0 {
		fmt.Fprintf(&b, "  FTR (ignored / judged): %.0f%%", s.FTR()*100)
		switch {
		case s.FTR() >= 0.5:
			b.WriteString(" — high over-intervention: most delivered cards go untouched\n")
		case s.FTR() >= 0.25:
			b.WriteString(" — watch: a quarter of cards go untouched\n")
		default:
			b.WriteString(" — healthy\n")
		}
	} else {
		b.WriteString("  FTR: not enough judged cards yet (all pending)\n")
	}
	if len(s.BySource) > 0 {
		sources := make([]string, 0, len(s.BySource))
		for src := range s.BySource {
			sources = append(sources, src)
		}
		sort.Slice(sources, func(i, j int) bool {
			if s.BySource[sources[i]] != s.BySource[sources[j]] {
				return s.BySource[sources[i]] > s.BySource[sources[j]]
			}
			return sources[i] < sources[j]
		})
		parts := make([]string, len(sources))
		for i, src := range sources {
			parts[i] = fmt.Sprintf("%s=%d", src, s.BySource[src])
		}
		b.WriteString("  ignored by source: " + strings.Join(parts, " ") + "\n")
	}
	return b.String()
}

// formatVllmPrefixCaches renders the engine-level prefix-cache hit rate, one
// line per served model. The counters are cumulative since vLLM boot — not
// scoped to the behavior window above. Empty input (no vLLM role, server
// down) renders nothing: the line simply does not appear.
func formatVllmPrefixCaches(stats []observe.VllmPrefixCache) string {
	var b strings.Builder
	for _, s := range stats {
		model := s.Model
		if model == "" {
			model = "vllm"
		}
		fmt.Fprintf(&b, "  prefix cache (%s, since engine boot): %d/%d (%.1f%%)\n",
			model, s.Hits, s.Queries, s.HitRatePct)
	}
	return b.String()
}
