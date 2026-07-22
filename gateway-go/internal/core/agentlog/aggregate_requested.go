package agentlog

import (
	"encoding/json"
	"path/filepath"
	"sort"
)

// RequestedModelStat rolls up token usage keyed by the model/role a run
// REQUESTED (RunEndData.RequestedModel) rather than the model that answered. It
// is the data source for a per-role usage breakdown: because the requested
// string is the role name ("submain"/"coding"/…) or "" for the default main, it
// separates roles that share one underlying model (e.g. glm serving both coding
// and submain). Runs recorded before RequestedModel existed carry "" and fold
// into the empty bucket, which the caller maps to the default (main) role.
type RequestedModelStat struct {
	RequestedModel  string `json:"requestedModel"`
	Runs            int    `json:"runs"`
	InputTokens     int64  `json:"inputTokens"`
	OutputTokens    int64  `json:"outputTokens"`
	CacheReadTokens int64  `json:"cacheReadTokens"`
	ToolCalls       int    `json:"toolCalls"`
}

// AggregateByRequestedModel folds every run.end under baseDir by its
// RequestedModel field. Unlike AggregateByModel it needs no run.start
// correlation — the requested string is carried on run.end itself. Entries
// older than sinceMs (when > 0) are skipped. Results are sorted by total
// (input+output) tokens descending.
func (w *Writer) AggregateByRequestedModel(sinceMs int64) []RequestedModelStat {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	stats := map[string]*RequestedModelStat{}
	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		for _, entry := range readAllEntries(path) {
			if sinceMs > 0 && entry.Ts < sinceMs {
				continue
			}
			var key string
			var in, out, cache, tools int
			switch entry.Type {
			case TypeRunEnd:
				var data RunEndData
				if json.Unmarshal(entry.Data, &data) != nil {
					continue
				}
				key = data.RequestedModel
				in, out, cache, tools = data.InputTokens, data.OutputTokens, data.CacheReadTokens, data.ToolCalls
			case TypeHelperLLM:
				// Helper calls request a role (lightweight/tiny/…); fold by that
				// role so local models used only for helpers appear per-role.
				var data HelperLLMData
				if json.Unmarshal(entry.Data, &data) != nil {
					continue
				}
				key = data.Role
				in, out, cache = data.InputTokens, data.OutputTokens, data.CacheReadTokens
			default:
				continue
			}
			s := stats[key]
			if s == nil {
				s = &RequestedModelStat{RequestedModel: key}
				stats[key] = s
			}
			s.Runs++
			s.InputTokens += int64(in)
			s.OutputTokens += int64(out)
			s.CacheReadTokens += int64(cache)
			s.ToolCalls += tools
		}
	}

	out := make([]RequestedModelStat, 0, len(stats))
	for _, s := range stats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].InputTokens + out[i].OutputTokens
		tj := out[j].InputTokens + out[j].OutputTokens
		if ti != tj {
			return ti > tj
		}
		return out[i].RequestedModel < out[j].RequestedModel
	})
	return out
}
