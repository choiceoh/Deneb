package agentlog

import (
	"encoding/json"
	"path/filepath"
	"sort"
)

// SessionStat is a per-session roll-up across recorded runs — the data source
// for the sessions tool's stats action ("어느 세션이 토큰을 먹었나"). Token
// figures come from run.end totals; there is no dollar-cost field anywhere in
// agentlog (tokens only), so cost is deliberately absent rather than derived
// from a price table that would drift.
type SessionStat struct {
	Session string `json:"session"`

	Runs   int `json:"runs"`   // run.end count
	Errors int `json:"errors"` // run.error count

	InputTokens     int64 `json:"inputTokens"`
	OutputTokens    int64 `json:"outputTokens"`
	CacheReadTokens int64 `json:"cacheReadTokens"`
	ToolCalls       int   `json:"toolCalls"`

	LastTs int64 `json:"lastTs"` // newest entry timestamp seen for the session
}

// AggregateBySession scans every session JSONL under baseDir and rolls up
// per-session run/token/tool totals from run.end (+ run.error) entries.
// Entries older than sinceMs (when > 0) are skipped. Sorted by total tokens
// (input+output) descending — the "who ate the budget" order.
func (w *Writer) AggregateBySession(sinceMs int64) []SessionStat {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	stats := map[string]*SessionStat{}
	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "*.jsonl"))
	for _, path := range paths {
		for _, e := range readAllEntries(path) {
			if sinceMs > 0 && e.Ts < sinceMs {
				continue
			}
			if e.Session == "" {
				continue
			}
			st := stats[e.Session]
			switch e.Type {
			case TypeRunEnd:
				if st == nil {
					st = &SessionStat{Session: e.Session}
					stats[e.Session] = st
				}
				var d RunEndData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				st.Runs++
				st.InputTokens += int64(d.InputTokens)
				st.OutputTokens += int64(d.OutputTokens)
				st.CacheReadTokens += int64(d.CacheReadTokens)
				st.ToolCalls += d.ToolCalls
			case TypeRunError:
				if st == nil {
					st = &SessionStat{Session: e.Session}
					stats[e.Session] = st
				}
				st.Errors++
			case TypeHelperLLM:
				if st == nil {
					st = &SessionStat{Session: e.Session}
					stats[e.Session] = st
				}
				var d HelperLLMData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				st.Runs++
				st.InputTokens += int64(d.InputTokens)
				st.OutputTokens += int64(d.OutputTokens)
				st.CacheReadTokens += int64(d.CacheReadTokens)
			default:
				continue
			}
			if e.Ts > st.LastTs {
				st.LastTs = e.Ts
			}
		}
	}

	out := make([]SessionStat, 0, len(stats))
	for _, st := range stats {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool {
		ti := out[i].InputTokens + out[i].OutputTokens
		tj := out[j].InputTokens + out[j].OutputTokens
		if ti != tj {
			return ti > tj
		}
		return out[i].Session < out[j].Session
	})
	return out
}
