package agentlog

import (
	"encoding/json"
	"path/filepath"
	"sort"
)

// ModelStat is a per-model behavioral roll-up across every recorded run —
// the data source for the background model tuner's scorecard. Attribution is
// by the model recorded at run.start (the *requested* model); a run that fell
// back mid-flight still counts against the model the failure belongs to.
type ModelStat struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`

	Runs   int `json:"runs"`   // run.end count
	Errors int `json:"errors"` // run.error count
	Turns  int `json:"turns"`  // summed turns across runs

	AvgMs int64 `json:"avgMs"` // mean run wall time
	P95Ms int64 `json:"p95Ms"` // 95th percentile run wall time

	InputTokens         int64 `json:"inputTokens"`
	OutputTokens        int64 `json:"outputTokens"`
	CacheReadTokens     int64 `json:"cacheReadTokens"`
	CacheCreationTokens int64 `json:"cacheCreationTokens"`

	// TimeoutRuns counts runs that ended with stopReason "timeout" — the
	// stall signature. MaxTokensRecoveries sums output-ceiling retries; a
	// persistently high ratio means the model needs a larger output budget.
	TimeoutRuns         int `json:"timeoutRuns"`
	MaxTokensRecoveries int `json:"maxTokensRecoveries"`
	CompactedRuns       int `json:"compactedRuns"`
	// FallbackRuns counts runs where a different model produced the answer
	// (run.end Model ≠ requested model) — how often this model needed rescue.
	FallbackRuns int `json:"fallbackRuns"`
	// ThinkingRuns counts runs whose run.start carried a non-off thinking
	// level — separates thinking-budget latency from genuinely slow models.
	ThinkingRuns int `json:"thinkingRuns"`

	ToolCalls  int `json:"toolCalls"`
	ToolErrors int `json:"toolErrors"`
}

// AggregateByModel scans every session JSONL under baseDir and rolls up
// per-model stats by correlating run.start (which carries model/provider)
// with the run's later entries via runId. A run.start is always written
// before its run.end/run.error/turn.* lines in the same file, so a single
// chronological pass per file suffices. Entries older than sinceMs (when
// > 0) are skipped. Results are sorted by run count descending.
func (w *Writer) AggregateByModel(sinceMs int64) []ModelStat {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	stats := map[string]*ModelStat{} // key: provider+"/"+model
	durations := map[string][]int64{}

	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		// runId → stats key / thinking flag, scoped per file (a run lands in
		// exactly one file).
		runModel := map[string]string{}
		runThinking := map[string]bool{}
		for _, e := range readAllEntries(path) {
			if sinceMs > 0 && e.Ts < sinceMs {
				continue
			}
			switch e.Type {
			case TypeRunStart:
				var d RunStartData
				if json.Unmarshal(e.Data, &d) != nil || d.Model == "" {
					continue
				}
				key := d.Provider + "/" + d.Model
				runModel[e.RunID] = key
				runThinking[e.RunID] = d.ThinkingLevel != "" && d.ThinkingLevel != "off"
				if stats[key] == nil {
					stats[key] = &ModelStat{Model: d.Model, Provider: d.Provider}
				}
			case TypeRunEnd:
				st := stats[runModel[e.RunID]]
				if st == nil {
					continue
				}
				var d RunEndData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				st.Runs++
				st.Turns += d.Turns
				st.InputTokens += int64(d.InputTokens)
				st.OutputTokens += int64(d.OutputTokens)
				st.CacheReadTokens += int64(d.CacheReadTokens)
				st.CacheCreationTokens += int64(d.CacheCreationTokens)
				st.MaxTokensRecoveries += d.MaxTokensRecoveries
				st.ToolCalls += d.ToolCalls
				if d.StopReason == "timeout" {
					st.TimeoutRuns++
				}
				if d.Model != "" && d.Model != st.Model {
					st.FallbackRuns++
				}
				if runThinking[e.RunID] {
					st.ThinkingRuns++
				}
				if d.Compacted {
					st.CompactedRuns++
				}
				key := runModel[e.RunID]
				durations[key] = append(durations[key], d.TotalMs)
			case TypeRunError:
				if st := stats[runModel[e.RunID]]; st != nil {
					st.Errors++
				}
			case TypeTurnTool:
				st := stats[runModel[e.RunID]]
				if st == nil {
					continue
				}
				var d TurnToolData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				if d.IsError {
					st.ToolErrors++
				}
			}
		}
	}

	out := make([]ModelStat, 0, len(stats))
	for key, st := range stats {
		if st.Runs == 0 && st.Errors == 0 {
			continue // run.start without any completion in the window
		}
		if ds := durations[key]; len(ds) > 0 {
			var total int64
			for _, d := range ds {
				total += d
			}
			st.AvgMs = total / int64(len(ds))
			sort.Slice(ds, func(i, j int) bool { return ds[i] < ds[j] })
			st.P95Ms = ds[(len(ds)*95)/100]
		}
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runs != out[j].Runs {
			return out[i].Runs > out[j].Runs
		}
		return out[i].Model < out[j].Model
	})
	return out
}
