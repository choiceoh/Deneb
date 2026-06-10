package modeltuner

import (
	"fmt"
	"strings"
)

// NoteFor renders a compact Korean stat line for one model from the latest
// scorecard window, for display under the native model picker entry. Empty
// when the scorecard has no runs for the model. tunedFloor (from
// modelrole.Registry.TunedMaxTokens) is appended when active.
func (sc Scorecard) NoteFor(model string, tunedFloor int) string {
	var parts []string
	for _, m := range sc.Models {
		if m.Model != model || m.Runs == 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%dh %d런", sc.WindowHours, m.Runs))
		parts = append(parts, fmt.Sprintf("p95 %.0f초", float64(m.P95Ms)/1000))
		if denom := m.CacheReadTokens + m.InputTokens; denom > 0 && m.CacheCreationTokens > 0 {
			parts = append(parts, fmt.Sprintf("캐시 %.0f%%", float64(m.CacheReadTokens)/float64(denom)*100))
		}
		if m.FallbackRuns > 0 {
			parts = append(parts, fmt.Sprintf("폴백 %d", m.FallbackRuns))
		}
		if m.TimeoutRuns > 0 {
			parts = append(parts, fmt.Sprintf("스톨 %d", m.TimeoutRuns))
		}
		break
	}
	if cal, ok := sc.Calibrations[model]; ok {
		mark := "✓"
		if !cal.KoreanOK {
			mark = "⚠한글"
		}
		parts = append(parts, fmt.Sprintf("프로브 %.1f초%s", float64(cal.LatencyMs)/1000, mark))
	}
	if tunedFloor > 0 {
		parts = append(parts, fmt.Sprintf("출력 floor %d", tunedFloor))
	}
	return strings.Join(parts, " · ")
}

// AdvisoryLines renders the open tuner recommendations as Korean display
// lines ("provider/model: message") for the native model picker header.
func (sc Scorecard) AdvisoryLines() []string {
	out := make([]string, 0, len(sc.Recommendations))
	for _, r := range sc.Recommendations {
		out = append(out, fmt.Sprintf("%s/%s: %s", r.Provider, r.Model, r.Message))
	}
	return out
}
