package agentlog

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// EffortStat rolls up adaptive effort-router decisions across recorded runs —
// the diagnostic for recalibrating the router's heuristics from real traffic
// instead of by feel. Only runs where the router was active (non-empty
// EffortDecision) are counted; the per-step observation sizes the router keys
// on are not in the run log, so this is a health/calibration view (is the
// router too aggressive or too timid?), not a direct per-constant retune.
//
// The load-bearing signal is EscalationRate: a routed-off run that had to
// restore thinking and retry means the router routed off when thinking was
// needed. A high rate says the gates should be stricter (route off less often);
// a near-zero RoutedShare says they are too strict (the router rarely fires).
type EffortStat struct {
	RoutedRuns    int `json:"routedRuns"`    // EffortDecision "routed:*" (thinking routed OFF)
	KeptRuns      int `json:"keptRuns"`      // EffortDecision "kept:*" (thinking kept ON)
	EscalatedRuns int `json:"escalatedRuns"` // routed-off runs that restored thinking and retried
	RoutedEndTurn int `json:"routedEndTurn"` // routed runs that finished cleanly (end_turn)
	RoutedTimeout int `json:"routedTimeout"` // routed runs that timed out

	// KeptReasons is a histogram of the gate that kept thinking on, keyed by the
	// category after "kept:" (e.g. "hard-signal", "context-heavy"). It shows
	// which gate carries the load.
	KeptReasons map[string]int `json:"keptReasons"`

	// Output-token sums per class — a rough savings proxy (routed-off runs spend
	// no reasoning tokens, so their mean output should be lower).
	RoutedOutputTokens int64 `json:"routedOutputTokens"`
	KeptOutputTokens   int64 `json:"keptOutputTokens"`
}

// EscalationRate is the fraction of routed-off runs that had to restore thinking
// — the router's false-route-off rate. 0 when no routed runs.
func (s EffortStat) EscalationRate() float64 {
	if s.RoutedRuns == 0 {
		return 0
	}
	return float64(s.EscalatedRuns) / float64(s.RoutedRuns)
}

// RoutedShare is the fraction of router-active runs where thinking was routed
// off. 0 when the router never ran.
func (s EffortStat) RoutedShare() float64 {
	total := s.RoutedRuns + s.KeptRuns
	if total == 0 {
		return 0
	}
	return float64(s.RoutedRuns) / float64(total)
}

// AggregateEffort scans every session JSONL under baseDir and rolls up the
// effort-router decision recorded on each run.end. EffortDecision lives on
// run.end itself, so unlike AggregateByModel this needs no run.start
// correlation — a single pass collecting run.end lines suffices. Entries older
// than sinceMs (when > 0) are skipped.
func (w *Writer) AggregateEffort(sinceMs int64) EffortStat {
	stat := EffortStat{KeptReasons: map[string]int{}}
	if w == nil {
		return stat
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scanEffort(sinceMs, func(_ string, d RunEndData) {
		accumulateEffort(&stat, d)
	})
	return stat
}

// AggregateEffortByModel buckets the same run.end effort decisions as
// AggregateEffort, keyed by the model that produced the run (RunEndData.Model).
// This is the per-model calibration view the adaptive-effort nudge keys on:
// EscalationRate/RoutedShare are router-gate signals that must be judged per
// model, since each model carries its own MaxSimpleRunes gate. Runs whose
// run.end carries no Model are bucketed under "" (and skipped by the nudge).
func (w *Writer) AggregateEffortByModel(sinceMs int64) map[string]EffortStat {
	out := map[string]EffortStat{}
	if w == nil {
		return out
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.scanEffort(sinceMs, func(model string, d RunEndData) {
		stat, ok := out[model]
		if !ok {
			stat = EffortStat{KeptReasons: map[string]int{}}
		}
		accumulateEffort(&stat, d)
		out[model] = stat
	})
	return out
}

// scanEffort walks every router-active run.end under baseDir (within sinceMs)
// and invokes fn with the run's model and decoded data. Callers must hold w.mu.
func (w *Writer) scanEffort(sinceMs int64, fn func(model string, d RunEndData)) {
	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		for _, e := range readAllEntries(path) {
			if e.Type != TypeRunEnd {
				continue
			}
			if sinceMs > 0 && e.Ts < sinceMs {
				continue
			}
			var d RunEndData
			if json.Unmarshal(e.Data, &d) != nil || d.EffortDecision == "" {
				continue
			}
			fn(d.Model, d)
		}
	}
}

// accumulateEffort folds one router-active run.end into stat. Shared by the
// global and per-model aggregators so they count identically.
func accumulateEffort(stat *EffortStat, d RunEndData) {
	switch {
	case strings.HasPrefix(d.EffortDecision, "routed:"):
		stat.RoutedRuns++
		stat.RoutedOutputTokens += int64(d.OutputTokens)
		if d.EffortEscalated {
			stat.EscalatedRuns++
		}
		switch d.StopReason {
		case "end_turn":
			stat.RoutedEndTurn++
		case "timeout":
			stat.RoutedTimeout++
		}
	case strings.HasPrefix(d.EffortDecision, "kept:"):
		stat.KeptRuns++
		stat.KeptOutputTokens += int64(d.OutputTokens)
		stat.KeptReasons[effortReasonCategory(d.EffortDecision)]++
	}
}

// effortReasonCategory pulls the gate category out of a "kept:<category>[:detail]"
// decision (e.g. "kept:hard-signal:분석" -> "hard-signal").
func effortReasonCategory(decision string) string {
	rest := strings.TrimPrefix(decision, "kept:")
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		return rest[:i]
	}
	if rest == "" {
		return "unknown"
	}
	return rest
}
