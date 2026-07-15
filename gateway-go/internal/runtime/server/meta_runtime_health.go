package server

// metaRuntimeHealthEvidence — the injected closure for MetaEvolutionTask's
// advisory runtime-health block (RSI P5-5). Reads the shared agentlog writer's
// 7d per-model aggregate and renders the dominant runtime signal: p95 latency,
// error rate, timeout/tool-error rate. Genesis stays a leaf — this closure
// owns all agentlog knowledge.
//
// ADVISORY ONLY: grounds the producer's prose on what the operator experiences.
// No gate reads it; nil/empty agentlog yields "" so a young runtime stays quiet.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/core/agentlog"
)

// metaRuntimeHealthWindow is the advisory lookback (matches the 7d evolution-
// health window the rest of the evidence block uses).
const metaRuntimeHealthWindow = 7 * 24 * time.Hour

// metaRuntimeHealthEvidence formats the 7d runtime composite as a one-liner
// the meta-evidence block appends. Empty when no runs are recorded yet (fresh
// install or dev without session logs).
func (s *Server) metaRuntimeHealthEvidence(_ context.Context) string {
	if s.agentLogWriter == nil {
		return ""
	}
	cutoff := time.Now().Add(-metaRuntimeHealthWindow).UnixMilli()
	stats := s.agentLogWriter.AggregateByModel(cutoff)
	if len(stats) == 0 {
		return "" // no runs in the window — quiet, not broken
	}
	// Collapse across models into fleet totals: the producer needs the overall
	// shape, not per-model breakdowns.
	var runs, errors, timeouts, toolCalls, toolErrors int
	var p95Max int64
	for _, st := range stats {
		runs += st.Runs
		errors += st.Errors
		timeouts += st.TimeoutRuns
		toolCalls += st.ToolCalls
		toolErrors += st.ToolErrors
		if st.P95Ms > p95Max {
			p95Max = st.P95Ms // worst-case tail across models
		}
	}
	if runs == 0 {
		return ""
	}
	// Rank the slowest model for context (the #1 weakness is usually latency).
	sort.Slice(stats, func(i, j int) bool { return stats[i].P95Ms > stats[j].P95Ms })
	var b strings.Builder
	fmt.Fprintf(&b, "- 최근 7일: %d런, p95 %dms (최고 지연 모델 %s %dms)",
		runs, p95Max, slowestModelName(stats), stats[0].P95Ms)
	if errors > 0 {
		fmt.Fprintf(&b, ", 오류율 %.1f%%", pct(errors, runs))
	}
	if timeouts > 0 {
		fmt.Fprintf(&b, ", 타임아웃 %d건", timeouts)
	}
	if toolCalls > 0 && toolErrors > 0 {
		fmt.Fprintf(&b, ", 도구오류 %.1f%%", pct(toolErrors, toolCalls))
	}
	b.WriteString("\n- 이는 운영자가 체감한 런타임 효용 자문 — 지연이 높으면 응답성 개선이 가치 있으나, 게이트 통과 여부와 무관.")
	return b.String()
}

// slowestModelName returns the short name of the slowest-by-p95 model, or a
// placeholder when the stat row lacks a model label.
func slowestModelName(stats []agentlog.ModelStat) string {
	if len(stats) == 0 || stats[0].Model == "" {
		return "?"
	}
	name := stats[0].Model
	// Shorten common prefixed names (e.g. "gpt-4o-2024-..." → keep up to first space).
	if i := strings.IndexByte(name, ' '); i > 0 {
		name = name[:i]
	}
	return name
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return 100.0 * float64(n) / float64(d)
}
