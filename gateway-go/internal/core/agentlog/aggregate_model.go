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
	// P95MsInteractive is the same percentile over USER-FACING runs only, and
	// InteractiveRuns is how many fed it.
	//
	// A p95 taken across a mixed population measures the largest budget cap in
	// the window, not how long anyone waited: a four-hour research cron and a
	// chat turn become one number. runtime_health.py hit this and fixed it by
	// splitting the populations ("what made the latency pillar score 0.0 on
	// every window"); regression-watch kept using the mixed figure and reported
	// p95@k3 = 836s on 2026-08-27 while user-facing p95 was 114s.
	//
	// Zero when no interactive filter is installed, so a caller that has not
	// wired one reads no signal rather than a wrong one.
	P95MsInteractive int64 `json:"p95MsInteractive,omitempty"`
	InteractiveRuns  int   `json:"interactiveRuns,omitempty"`

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
// SetInteractiveSessionFilter installs the classifier that decides which
// sessions are user-facing, enabling the P95MsInteractive figure.
//
// Injected rather than implemented here because the run-kind vocabulary belongs
// to domain/session and this is a core package. Copying the rule would give the
// codebase two answers to "is this a user turn", and the copy is the one that
// goes stale.
func (w *Writer) SetInteractiveSessionFilter(fn func(sessionKey string) bool) {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.interactive = fn
	w.mu.Unlock()
}

func (w *Writer) AggregateByModel(sinceMs int64) []ModelStat {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	accumulator := newModelAggregateAccumulator(w.interactive)
	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		accumulator.foldFile(readAllEntries(path), sinceMs)
	}
	return accumulator.finish()
}

type modelAggregateAccumulator struct {
	stats     map[string]*ModelStat
	durations map[string][]int64
	// interactiveDurations is the user-facing subset, accumulated in the same
	// single pass so the split costs no extra file scan.
	interactiveDurations map[string][]int64
	interactive          func(sessionKey string) bool
}

type modelRunScope struct {
	modelKeys map[string]string
	thinking  map[string]bool
}

func newModelAggregateAccumulator(interactive func(string) bool) *modelAggregateAccumulator {
	return &modelAggregateAccumulator{
		stats:                map[string]*ModelStat{},
		durations:            map[string][]int64{},
		interactiveDurations: map[string][]int64{},
		interactive:          interactive,
	}
}

// foldFile keeps runId correlation scoped to one session JSONL. Reusing a run
// ID in another session cannot reattribute its completion to this file's model.
func (a *modelAggregateAccumulator) foldFile(entries []LogEntry, sinceMs int64) {
	scope := modelRunScope{
		modelKeys: map[string]string{},
		thinking:  map[string]bool{},
	}
	for _, entry := range entries {
		if sinceMs > 0 && entry.Ts < sinceMs {
			continue
		}
		a.foldEntry(entry, &scope)
	}
}

func (a *modelAggregateAccumulator) foldEntry(entry LogEntry, scope *modelRunScope) {
	switch entry.Type {
	case TypeRunStart:
		a.foldRunStart(entry, scope)
	case TypeRunEnd:
		a.foldModelRunEnd(entry, scope)
	case TypeRunError:
		a.foldModelRunError(entry, scope)
	case TypeTurnTool:
		a.foldModelTool(entry, scope)
	case TypeHelperLLM:
		a.foldModelHelper(entry)
	}
}

func (a *modelAggregateAccumulator) foldRunStart(entry LogEntry, scope *modelRunScope) {
	var data RunStartData
	if json.Unmarshal(entry.Data, &data) != nil || data.Model == "" {
		return
	}
	key := data.Provider + "/" + data.Model
	scope.modelKeys[entry.RunID] = key
	scope.thinking[entry.RunID] = data.ThinkingLevel != "" && data.ThinkingLevel != "off"
	if a.stats[key] == nil {
		a.stats[key] = &ModelStat{Model: data.Model, Provider: data.Provider}
	}
}

func (a *modelAggregateAccumulator) statForRun(entry LogEntry, scope *modelRunScope) *ModelStat {
	return a.stats[scope.modelKeys[entry.RunID]]
}

func (a *modelAggregateAccumulator) foldModelRunEnd(entry LogEntry, scope *modelRunScope) {
	stat := a.statForRun(entry, scope)
	if stat == nil {
		return
	}
	var data RunEndData
	if json.Unmarshal(entry.Data, &data) != nil {
		return
	}
	stat.Runs++
	stat.Turns += data.Turns
	stat.InputTokens += int64(data.InputTokens)
	stat.OutputTokens += int64(data.OutputTokens)
	stat.CacheReadTokens += int64(data.CacheReadTokens)
	stat.CacheCreationTokens += int64(data.CacheCreationTokens)
	stat.MaxTokensRecoveries += data.MaxTokensRecoveries
	stat.ToolCalls += data.ToolCalls
	if data.StopReason == "timeout" {
		stat.TimeoutRuns++
	}
	if data.Model != "" && data.Model != stat.Model {
		stat.FallbackRuns++
	}
	if scope.thinking[entry.RunID] {
		stat.ThinkingRuns++
	}
	if data.Compacted {
		stat.CompactedRuns++
	}
	key := scope.modelKeys[entry.RunID]
	a.durations[key] = append(a.durations[key], data.TotalMs)
	// The run record carries its own session key, so classification needs no
	// new field and no file-name parsing. An entry with no session stays out of
	// the interactive population — unattributed is not user-facing.
	if a.interactive != nil && entry.Session != "" && a.interactive(entry.Session) {
		a.interactiveDurations[key] = append(a.interactiveDurations[key], data.TotalMs)
	}
}

func (a *modelAggregateAccumulator) foldModelRunError(entry LogEntry, scope *modelRunScope) {
	if stat := a.statForRun(entry, scope); stat != nil {
		stat.Errors++
	}
}

func (a *modelAggregateAccumulator) foldModelTool(entry LogEntry, scope *modelRunScope) {
	stat := a.statForRun(entry, scope)
	if stat == nil {
		return
	}
	var data TurnToolData
	if json.Unmarshal(entry.Data, &data) != nil {
		return
	}
	if data.IsError {
		stat.ToolErrors++
	}
}

// foldModelHelper credits a helper.llm event's tokens directly to its model
// stat (no run.start correlation — the event is self-contained). This is how
// local models used only for one-shot helper calls appear in per-model usage.
func (a *modelAggregateAccumulator) foldModelHelper(entry LogEntry) {
	var data HelperLLMData
	if json.Unmarshal(entry.Data, &data) != nil || data.Model == "" {
		return
	}
	key := data.Provider + "/" + data.Model
	stat := a.stats[key]
	if stat == nil {
		stat = &ModelStat{Model: data.Model, Provider: data.Provider}
		a.stats[key] = stat
	}
	stat.Runs++
	stat.InputTokens += int64(data.InputTokens)
	stat.OutputTokens += int64(data.OutputTokens)
	stat.CacheReadTokens += int64(data.CacheReadTokens)
}

func (a *modelAggregateAccumulator) finish() []ModelStat {
	out := make([]ModelStat, 0, len(a.stats))
	for key, stat := range a.stats {
		if stat.Runs == 0 && stat.Errors == 0 {
			continue
		}
		finalizeModelLatency(stat, a.durations[key])
		finalizeInteractiveLatency(stat, a.interactiveDurations[key])
		out = append(out, *stat)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Runs != out[j].Runs {
			return out[i].Runs > out[j].Runs
		}
		return out[i].Model < out[j].Model
	})
	return out
}

func finalizeModelLatency(stat *ModelStat, durations []int64) {
	if len(durations) == 0 {
		return
	}
	var total int64
	for _, duration := range durations {
		total += duration
	}
	stat.AvgMs = total / int64(len(durations))
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	stat.P95Ms = durations[(len(durations)*95)/100]
}

// finalizeInteractiveLatency fills the user-facing percentile, leaving it zero
// when nothing interactive was seen — so a consumer can tell "no user turns in
// this window" from "user turns were fast".
func finalizeInteractiveLatency(stat *ModelStat, durations []int64) {
	if len(durations) == 0 {
		return
	}
	stat.InteractiveRuns = len(durations)
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	stat.P95MsInteractive = durations[(len(durations)*95)/100]
}
