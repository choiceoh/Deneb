package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
	"github.com/choiceoh/deneb/gateway-go/internal/runtime/observe"
	"github.com/choiceoh/deneb/gateway-go/pkg/jsonutil"
)

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
func ToolObserve(lc *observe.LogCapture, alog *agentlog.Writer, vllmBases func() []string) ToolFunc {
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

		default:
			return "", fmt.Errorf("observe: unknown action %q — use turn | logs | behavior", p.Action)
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
