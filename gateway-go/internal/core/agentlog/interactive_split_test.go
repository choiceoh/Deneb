package agentlog

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// TestInteractiveSplitSeparatesPopulations: a slow automation run must not
// enter the user-facing percentile, and vice versa.
func TestInteractiveSplitSeparatesPopulations(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	w.SetInteractiveSessionFilter(func(key string) bool {
		return strings.HasPrefix(key, "client:")
	})

	write := func(sessionKey, runID string, totalMs int64) {
		t.Helper()
		start, _ := json.Marshal(RunStartData{Model: "k3", Provider: "p"})
		end, _ := json.Marshal(RunEndData{Model: "k3", TotalMs: totalMs})
		if err := w.Append(LogEntry{Ts: 1, Type: TypeRunStart, RunID: runID, Session: sessionKey, Data: start}); err != nil {
			t.Fatalf("append start: %v", err)
		}
		if err := w.Append(LogEntry{Ts: 2, Type: TypeRunEnd, RunID: runID, Session: sessionKey, Data: end}); err != nil {
			t.Fatalf("append end: %v", err)
		}
	}
	// One fast chat turn, one four-hour research cron.
	write("client:main", "r1", 5_000)
	write("cron:research", "r2", 14_400_000)

	stats := w.AggregateByModel(0)
	if len(stats) == 0 {
		t.Fatalf("no stats produced (files: %v)", listDir(t, dir))
	}
	s := stats[0]
	if s.InteractiveRuns != 1 {
		t.Fatalf("interactiveRuns = %d, want 1", s.InteractiveRuns)
	}
	if s.P95MsInteractive != 5_000 {
		t.Errorf("interactive p95 = %d, want 5000 — the cron must not enter the user-facing population", s.P95MsInteractive)
	}
	if s.P95Ms < 14_400_000 {
		t.Errorf("mixed p95 = %d, want the cron still counted in the full population", s.P95Ms)
	}
}

// TestNoFilterMeansNoInteractiveFigure: an unwired classifier must yield zero
// rather than a guess, so a consumer reads no signal instead of a wrong one.
func TestNoFilterMeansNoInteractiveFigure(t *testing.T) {
	dir := t.TempDir()
	w := NewWriter(dir)
	start, _ := json.Marshal(RunStartData{Model: "k3", Provider: "p"})
	end, _ := json.Marshal(RunEndData{Model: "k3", TotalMs: 5_000})
	_ = w.Append(LogEntry{Ts: 1, Type: TypeRunStart, RunID: "r1", Session: "client:main", Data: start})
	_ = w.Append(LogEntry{Ts: 2, Type: TypeRunEnd, RunID: "r1", Session: "client:main", Data: end})

	for _, s := range w.AggregateByModel(0) {
		if s.InteractiveRuns != 0 || s.P95MsInteractive != 0 {
			t.Errorf("unwired filter produced interactive figures: %+v", s)
		}
	}
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	return matches
}
