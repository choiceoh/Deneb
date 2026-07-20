package agentlog

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// HighEffortRun is one real-user run that completed normally but ground
// through an unusually high number of tool calls. Where FailedUserRequests
// mines EXPLICIT capability gaps (the agent could not do it), this mines the
// implicit half of the demand signal: the agent did it, but the hard way — a
// request shape that recurs at this cost is a skill candidate. Pattern
// adopted from SearchOS's host miner (arXiv 2607.15257: hosts opened
// repeatedly with poor generic yield get a purpose-built access skill;
// 2026-07-20 review).
type HighEffortRun struct {
	Session   string `json:"session"`
	Message   string `json:"message"`   // run.start user input (write-truncated)
	ToolCalls int    `json:"toolCalls"` // total tool_use blocks across the run
	Turns     int    `json:"turns"`
	TotalMs   int64  `json:"totalMs"`
	TopTools  string `json:"topTools"`  // "wiki×6 · exec×3" — top-3 of the histogram
	UsedSkill bool   `json:"usedSkill"` // the skills tool was consulted mid-run
	Ts        int64  `json:"ts"`        //nolint:staticcheck // ST1003 — matches LogEntry
}

// skillsToolName is the catalog-consult tool as it appears in ToolCounts.
const skillsToolName = "skills"

// highEffortTopTools bounds the rendered histogram summary.
const highEffortTopTools = 3

// HighEffortUserRuns scans REAL client sessions ("client:*"; the live-test
// synthetic prefix is skipped, system:*/cron:* never match the glob) for runs
// since sinceMs that ended cleanly (end_turn, not proactive) yet spent at
// least minToolCalls tool calls, joins each to its run's user message, and
// returns them heaviest-first (ToolCalls desc, then newest), deduped by
// message text keeping the heaviest instance, capped at limit. Runs whose
// run.start message is empty or already rotated away are dropped — a request
// we cannot quote is unusable as demand evidence (the curriculum's
// verbatim-quote grounding gate binds proposals to the request text).
func (w *Writer) HighEffortUserRuns(sinceMs int64, minToolCalls, limit int) []HighEffortRun {
	if w == nil || limit <= 0 || minToolCalls <= 0 {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	paths, _ := filepath.Glob(filepath.Join(w.baseDir, "client:*.jsonl"))
	var out []HighEffortRun
	for _, path := range paths {
		if strings.HasPrefix(filepath.Base(path), liveTestSessionPrefix) {
			continue
		}
		starts := map[string]string{} // runId -> user message
		for _, e := range readAllEntries(path) {
			switch e.Type {
			case TypeRunStart:
				var d RunStartData
				if json.Unmarshal(e.Data, &d) == nil {
					starts[e.RunID] = strings.TrimSpace(d.Message)
				}
			case TypeRunEnd:
				if e.Ts < sinceMs {
					continue
				}
				var d RunEndData
				if json.Unmarshal(e.Data, &d) != nil {
					continue
				}
				if d.Proactive || d.StopReason != "end_turn" || d.ToolCalls < minToolCalls {
					continue
				}
				msg := starts[e.RunID]
				if msg == "" {
					continue
				}
				out = append(out, HighEffortRun{
					Session:   e.Session,
					Message:   msg,
					ToolCalls: d.ToolCalls,
					Turns:     d.Turns,
					TotalMs:   d.TotalMs,
					TopTools:  topToolsSummary(d.ToolCounts, highEffortTopTools),
					UsedSkill: d.ToolCounts[skillsToolName] > 0,
					Ts:        e.Ts,
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ToolCalls != out[j].ToolCalls {
			return out[i].ToolCalls > out[j].ToolCalls
		}
		return out[i].Ts > out[j].Ts
	})
	seen := map[string]bool{}
	deduped := out[:0]
	for _, r := range out {
		if seen[r.Message] {
			continue
		}
		seen[r.Message] = true
		deduped = append(deduped, r)
		if len(deduped) >= limit {
			break
		}
	}
	return deduped
}

// topToolsSummary renders the n most-used tools as "name×count" joined with
// " · ", count desc then name asc for a deterministic order.
func topToolsSummary(counts map[string]int, n int) string {
	if len(counts) == 0 || n <= 0 {
		return ""
	}
	type kv struct {
		name  string
		count int
	}
	sorted := make([]kv, 0, len(counts))
	for name, c := range counts {
		if c > 0 {
			sorted = append(sorted, kv{name, c})
		}
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].name < sorted[j].name
	})
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	parts := make([]string, len(sorted))
	for i, t := range sorted {
		parts[i] = fmt.Sprintf("%s×%d", t.name, t.count)
	}
	return strings.Join(parts, " · ")
}
