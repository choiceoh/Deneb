package observe

import (
	"encoding/json"
	"log/slog"

	"github.com/choiceoh/deneb/gateway-go/internal/agentsys/agentlog"
)

// TurnView is the whole shape of one agent run, joined from two sources that
// were never connected before: the agentlog turn-shape (what the agent did —
// tokens, tools, cache, compaction) and the captured slog lines (what the
// runtime logged while doing it). This is the answer to "what happened on run
// X, and why" in a single object.
type TurnView struct {
	RunID   string `json:"runId"`
	Session string `json:"session,omitempty"`
	// Found is true when agentlog had structured events for this run. A run can
	// still have Logs without being Found (e.g. an old run whose JSONL rotated
	// away but whose log lines are still in the ring) — the agent then works off
	// the raw log alone.
	Found bool `json:"found"`

	Start   *agentlog.RunStartData `json:"start,omitempty"`
	Prep    *agentlog.RunPrepData  `json:"prep,omitempty"`
	End     *agentlog.RunEndData   `json:"end,omitempty"` // the turn shape
	Failure *agentlog.RunErrorData `json:"error,omitempty"`

	Turns []agentlog.TurnLLMData  `json:"turns,omitempty"`
	Tools []agentlog.TurnToolData `json:"tools,omitempty"`

	// Logs are the captured slog lines for this run, newest-first. Capped so a
	// chatty run can't return an unbounded payload.
	Logs []LogLine `json:"logs,omitempty"`
}

// turnLogLimit caps how many captured log lines a single turn view carries.
const turnLogLimit = 500

// BuildTurnView assembles the view for runID. alog and ring are each optional
// (nil-safe): with neither, the result is an empty, not-found view.
func BuildTurnView(alog *agentlog.Writer, ring *Ring, runID string) TurnView {
	view := TurnView{RunID: runID}

	entries, session := alog.ReadRun(runID) // nil-safe
	view.Session = session
	for _, e := range entries {
		view.Found = true
		applyEntry(&view, e)
	}

	if ring != nil {
		view.Logs = ring.Query(QueryOpts{
			RunID:    runID,
			MinLevel: slog.LevelDebug, // a turn view wants everything, not just errors
			Limit:    turnLogLimit,
		})
		// If agentlog had nothing (rotated away) we can still recover the
		// session from the captured logs, which carry it too.
		if view.Session == "" {
			for _, l := range view.Logs {
				if l.Session != "" {
					view.Session = l.Session
					break
				}
			}
		}
	}
	return view
}

// applyEntry decodes one agentlog entry into the right slot of the view.
// Unknown / standalone-event types are ignored — a turn view is per-run, and
// proactive.relay / background.job belong to the behavior aggregate instead.
func applyEntry(view *TurnView, e agentlog.LogEntry) {
	switch e.Type {
	case agentlog.TypeRunStart:
		var d agentlog.RunStartData
		if json.Unmarshal(e.Data, &d) == nil {
			view.Start = &d
		}
	case agentlog.TypeRunPrep:
		var d agentlog.RunPrepData
		if json.Unmarshal(e.Data, &d) == nil {
			view.Prep = &d
		}
	case agentlog.TypeTurnLLM:
		var d agentlog.TurnLLMData
		if json.Unmarshal(e.Data, &d) == nil {
			view.Turns = append(view.Turns, d)
		}
	case agentlog.TypeTurnTool:
		var d agentlog.TurnToolData
		if json.Unmarshal(e.Data, &d) == nil {
			view.Tools = append(view.Tools, d)
		}
	case agentlog.TypeRunEnd:
		var d agentlog.RunEndData
		if json.Unmarshal(e.Data, &d) == nil {
			view.End = &d
		}
	case agentlog.TypeRunError:
		var d agentlog.RunErrorData
		if json.Unmarshal(e.Data, &d) == nil {
			view.Failure = &d
		}
	}
}
