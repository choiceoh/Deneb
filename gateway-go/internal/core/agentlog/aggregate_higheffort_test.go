package agentlog

import (
	"encoding/json"
	"testing"
)

func endEntry(t *testing.T, session, runID string, ts int64, d RunEndData) LogEntry {
	t.Helper()
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return LogEntry{Ts: ts, Type: TypeRunEnd, RunID: runID, Session: session, Data: data}
}

func cleanEnd(toolCalls int) RunEndData {
	return RunEndData{StopReason: "end_turn", Turns: 3, TotalMs: 12000, ToolCalls: toolCalls}
}

// HighEffortUserRuns selects only completed, non-proactive, real-client runs
// at or above the tool-call floor, joined to a quotable user message.
func TestHighEffortUserRuns_Filters(t *testing.T) {
	dir := t.TempDir()
	proactive := cleanEnd(20)
	proactive.Proactive = true
	timedOut := cleanEnd(20)
	timedOut.StopReason = "timeout"
	writeSessionLog(t, dir, "client:main", []LogEntry{
		startEntry(t, "client:main", "r1", "무거운 요청 하나", 100),
		endEntry(t, "client:main", "r1", 110, cleanEnd(12)),
		startEntry(t, "client:main", "r2", "가벼운 요청", 200),
		endEntry(t, "client:main", "r2", 210, cleanEnd(3)), // below floor
		startEntry(t, "client:main", "r3", "자율 실행 요청", 300),
		endEntry(t, "client:main", "r3", 310, proactive), // proactive
		startEntry(t, "client:main", "r4", "타임아웃 요청", 400),
		endEntry(t, "client:main", "r4", 410, timedOut),     // not end_turn
		endEntry(t, "client:main", "r5", 510, cleanEnd(15)), // no joinable message
		startEntry(t, "client:main", "r6", "옛날 요청", 10),
		endEntry(t, "client:main", "r6", 20, cleanEnd(15)), // before window
	})
	writeSessionLog(t, dir, "client:lt-9", []LogEntry{ // live-test synthetic
		startEntry(t, "client:lt-9", "r7", "합성 요청", 600),
		endEntry(t, "client:lt-9", "r7", 610, cleanEnd(15)),
	})

	w := NewWriter(dir)
	got := w.HighEffortUserRuns(50, 8, 10)
	if len(got) != 1 {
		t.Fatalf("expected exactly the one qualifying run, got %d: %+v", len(got), got)
	}
	if got[0].Message != "무거운 요청 하나" || got[0].ToolCalls != 12 || got[0].Turns != 3 {
		t.Errorf("run fields wrong: %+v", got[0])
	}
}

// Results order heaviest-first and dedup by message keeping the heaviest
// instance; the cap is honored.
func TestHighEffortUserRuns_OrderDedupCap(t *testing.T) {
	dir := t.TempDir()
	writeSessionLog(t, dir, "client:main", []LogEntry{
		startEntry(t, "client:main", "a", "반복 요청", 100),
		endEntry(t, "client:main", "a", 110, cleanEnd(9)),
		startEntry(t, "client:main", "b", "반복 요청", 200),
		endEntry(t, "client:main", "b", 210, cleanEnd(16)), // heaviest duplicate wins
		startEntry(t, "client:main", "c", "다른 요청", 300),
		endEntry(t, "client:main", "c", 310, cleanEnd(12)),
	})

	w := NewWriter(dir)
	got := w.HighEffortUserRuns(0, 8, 10)
	if len(got) != 2 {
		t.Fatalf("duplicates must collapse: got %d: %+v", len(got), got)
	}
	if got[0].Message != "반복 요청" || got[0].ToolCalls != 16 {
		t.Errorf("heaviest instance should lead: %+v", got[0])
	}
	if got[1].Message != "다른 요청" {
		t.Errorf("second run wrong: %+v", got[1])
	}

	if capped := w.HighEffortUserRuns(0, 8, 1); len(capped) != 1 {
		t.Errorf("limit must cap results, got %d", len(capped))
	}
}

// The tool histogram renders top-N deterministically and the skills-tool
// consult flag is derived from it.
func TestHighEffortUserRuns_TopToolsAndSkillFlag(t *testing.T) {
	dir := t.TempDir()
	d := cleanEnd(10)
	d.ToolCounts = map[string]int{"wiki": 5, "exec": 3, "skills": 1, "read": 1}
	writeSessionLog(t, dir, "client:main", []LogEntry{
		startEntry(t, "client:main", "r1", "스킬 참조한 무거운 요청", 100),
		endEntry(t, "client:main", "r1", 110, d),
	})

	w := NewWriter(dir)
	got := w.HighEffortUserRuns(0, 8, 10)
	if len(got) != 1 {
		t.Fatalf("expected 1 run, got %d", len(got))
	}
	if got[0].TopTools != "wiki×5 · exec×3 · read×1" {
		t.Errorf("top tools = %q (ties break by name asc)", got[0].TopTools)
	}
	if !got[0].UsedSkill {
		t.Error("skills consult must set UsedSkill")
	}
}

func TestTopToolsSummary_Empty(t *testing.T) {
	if got := topToolsSummary(nil, 3); got != "" {
		t.Errorf("empty histogram should render empty, got %q", got)
	}
}
