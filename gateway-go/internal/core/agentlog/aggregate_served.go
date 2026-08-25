package agentlog

import (
	"encoding/json"
	"path/filepath"
	"sort"
)

// ServedModelStat rolls up token usage keyed by the model the PROVIDER
// reported serving (turn.llm ProviderModel) rather than the model the run
// asked for. The two differ whenever an endpoint aliases a pinned id to a
// newer weight set — e.g. a sub-agent pinned to "glm-5.1" whose every turn is
// answered by glm-5.3 — so a usage panel keyed by the requested id reports a
// model that never ran. Attribution of *failures* still belongs to the
// requested model (see AggregateByModel); this view answers the different
// question of which weights actually burned the tokens.
type ServedModelStat struct {
	Model    string `json:"model"`
	Provider string `json:"provider,omitempty"`

	Runs            int   `json:"runs"`
	InputTokens     int64 `json:"inputTokens"`
	OutputTokens    int64 `json:"outputTokens"`
	CacheReadTokens int64 `json:"cacheReadTokens"`
	ToolCalls       int   `json:"toolCalls"`
}

// AggregateByServedModel scans every session JSONL under baseDir and folds
// per-turn usage by the serving model. Tokens come from turn.llm (whose totals
// sum to run.end's), so a run whose fallback chain fired mid-flight splits
// across both models instead of landing wholly on one. Runs counts each run
// once per model that answered a turn in it — a run served by one model counts
// as one run, as it did before. A turn whose provider reported no model, or one
// whose run.start fell outside the window, keeps the requested model as its
// label. helper.llm events are credited directly: they carry the answering
// model and have no turns of their own. Entries older than sinceMs (when > 0)
// are skipped. Results are sorted by total (input+output) tokens descending.
func (w *Writer) AggregateByServedModel(sinceMs int64) []ServedModelStat {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	stats := map[string]*ServedModelStat{}
	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		foldServedFile(stats, readAllEntries(path), sinceMs)
	}

	out := make([]ServedModelStat, 0, len(stats))
	for _, s := range stats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].InputTokens + out[i].OutputTokens
		tj := out[j].InputTokens + out[j].OutputTokens
		if ti != tj {
			return ti > tj
		}
		return out[i].Model < out[j].Model
	})
	return out
}

// servedRunScope keeps run correlation inside one session JSONL, mirroring
// AggregateByModel: a run ID reused in another session cannot borrow this
// file's requested model as its fallback label.
type servedRunScope struct {
	requested map[string]ServedModelStat // runID -> provider/model from run.start
	counted   map[string]map[string]bool // runID -> served key -> already counted as a run
}

func foldServedFile(stats map[string]*ServedModelStat, entries []LogEntry, sinceMs int64) {
	scope := servedRunScope{
		requested: map[string]ServedModelStat{},
		counted:   map[string]map[string]bool{},
	}
	for _, entry := range entries {
		if sinceMs > 0 && entry.Ts < sinceMs {
			continue
		}
		switch entry.Type {
		case TypeRunStart:
			var data RunStartData
			if json.Unmarshal(entry.Data, &data) != nil || data.Model == "" {
				continue
			}
			// A run ID is only unique per run *within a session file*: the
			// per-session counter restarts at run_0000 when the gateway does,
			// so a file can hold several runs under one ID. run.start opens a
			// fresh scope for the ID so the later run counts as its own run.
			scope.requested[entry.RunID] = ServedModelStat{Model: data.Model, Provider: data.Provider}
			delete(scope.counted, entry.RunID)
		case TypeTurnLLM:
			var data TurnLLMData
			if json.Unmarshal(entry.Data, &data) != nil {
				continue
			}
			foldServedTurn(stats, &scope, entry.RunID, data)
		case TypeRunEnd:
			var data RunEndData
			if json.Unmarshal(entry.Data, &data) != nil {
				continue
			}
			foldServedRunEnd(stats, &scope, entry.RunID, data)
			delete(scope.counted, entry.RunID)
			delete(scope.requested, entry.RunID)
		case TypeHelperLLM:
			var data HelperLLMData
			if json.Unmarshal(entry.Data, &data) != nil || data.Model == "" {
				continue
			}
			s := servedStatFor(stats, data.Provider, data.Model)
			s.Runs++
			s.InputTokens += int64(data.InputTokens)
			s.OutputTokens += int64(data.OutputTokens)
			s.CacheReadTokens += int64(data.CacheReadTokens)
		}
	}
}

func foldServedTurn(stats map[string]*ServedModelStat, scope *servedRunScope, runID string, data TurnLLMData) {
	requested := scope.requested[runID]
	model, provider := data.ProviderModel, requested.Provider
	if model == "" {
		// Provider reported nothing (or the run.start is outside the window):
		// the requested id is the best label available. Both empty means the
		// turn is unattributable, so drop it rather than invent a bucket.
		model = requested.Model
		if model == "" {
			return
		}
	}
	s := servedStatFor(stats, provider, model)
	key := provider + "/" + model
	if scope.counted[runID] == nil {
		scope.counted[runID] = map[string]bool{}
	}
	if !scope.counted[runID][key] {
		scope.counted[runID][key] = true
		s.Runs++
	}
	s.InputTokens += int64(data.InputTokens)
	s.OutputTokens += int64(data.OutputTokens)
	s.CacheReadTokens += int64(data.CacheReadTokens)
	s.ToolCalls += data.ToolCalls
}

// foldServedRunEnd rescues a run no turn line accounted for — an aborted run
// with no completed turn, or one whose turns predate the window. Without it a
// zero-token run would silently drop out of the run count. The label comes from
// run.end's answering model (the fallback chain's result), falling back to the
// requested one.
func foldServedRunEnd(stats map[string]*ServedModelStat, scope *servedRunScope, runID string, data RunEndData) {
	if len(scope.counted[runID]) > 0 {
		return // already accounted for, turn by turn
	}
	requested := scope.requested[runID]
	model, provider := data.Model, requested.Provider
	if model == "" {
		model = requested.Model
		if model == "" {
			return
		}
	}
	s := servedStatFor(stats, provider, model)
	s.Runs++
	s.InputTokens += int64(data.InputTokens)
	s.OutputTokens += int64(data.OutputTokens)
	s.CacheReadTokens += int64(data.CacheReadTokens)
	s.ToolCalls += data.ToolCalls
}

func servedStatFor(stats map[string]*ServedModelStat, provider, model string) *ServedModelStat {
	key := provider + "/" + model
	if stats[key] == nil {
		stats[key] = &ServedModelStat{Model: model, Provider: provider}
	}
	return stats[key]
}
