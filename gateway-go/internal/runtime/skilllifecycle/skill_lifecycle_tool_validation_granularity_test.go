package skilllifecycle

import (
	"strings"
	"testing"

	chattools "github.com/choiceoh/deneb/gateway-go/internal/pipeline/chat/tools/lifecycletool"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/skills/genesis/generation"
)

func backfillRequest() chattools.SkillValidationCaseFromSessionRequest {
	return chattools.SkillValidationCaseFromSessionRequest{
		SkillName:  "topsolar-db",
		SessionKey: "client:main:abc",
		Source:     "auto-backfill-lane",
	}
}

// TestSessionCaseKeepsUnitGranularity pins the fixture shape the behavioral gate
// can actually score. A session is a multi-turn trajectory; the gate replays one
// plan. Recording the trajectory verbatim produced cases with ~11 distinct
// shapes, session-bound argument substrings and a full ordering requirement —
// fixtures the UNCHANGED skill itself only satisfied 15-63% of, which is why the
// gate rejected 15 consecutive candidates without separating good from bad.
func TestSessionCaseKeepsUnitGranularity(t *testing.T) {
	activities := []generation.ToolActivity{
		{Name: "exec", Input: "topsolar.py dashboard"},
		{Name: "exec", Input: "topsolar.py dashboard"},
		{Name: "exec", Input: "topsolar.py dashboard"},
		{Name: "exec", Input: "topsolar.py dashboard"},
		{Name: "exec", Input: "topsolar.py dashboard"},
		{Name: "exec", Input: "topsolar.py dashboard"},
		{Name: "exec", Input: "topsolar.py dashboard"},
		{Name: "read", Input: "/home/choiceoh/notes-2026-07-04.md"},
		{Name: "wiki", Input: "당진 솔라빌리지"},
		{Name: "calendar", Input: "today"},
		{Name: "mail_archive", Input: "thread"},
	}
	rec := buildSkillValidationCaseFromSession(backfillRequest(), generation.SessionContext{
		Key:            "client:main:abc",
		AllText:        "프로젝트 현황 알려줘",
		ToolActivities: activities,
	})
	calls := rec.Replay.ExpectedToolCalls

	if len(calls) > maxDistinctReplayShapes {
		t.Fatalf("expected at most %d distinct shapes, got %d: %+v",
			maxDistinctReplayShapes, len(calls), calls)
	}

	seen := map[string]int{}
	for _, call := range calls {
		seen[replayCallShapeKey(call)]++
	}
	for key, n := range seen {
		if n > 1 {
			t.Errorf("shape %q recorded %d times; repeats add no signal to an existence match", key, n)
		}
	}

	for _, call := range calls {
		for _, include := range call.InputIncludes {
			if replayVolatileFragment.MatchString(include) {
				t.Errorf("call %q kept a session-bound argument %q; a fresh replay can never emit it",
					call.Name, include)
			}
		}
	}

	// The volatile read still asserts the TOOL — dropping the argument must not
	// drop the observation.
	var sawRead bool
	for _, call := range calls {
		if call.Name == "read" {
			sawRead = true
			if len(call.InputIncludes) != 0 {
				t.Errorf("read call should keep the tool but no argument, got %+v", call.InputIncludes)
			}
		}
	}
	if !sawRead {
		t.Errorf("read call was dropped entirely; expected the tool to survive: %+v", calls)
	}

	if rec.Replay.RequireOrder {
		t.Errorf("a %d-call trace must not assert ordering; one plan cannot lay out a whole session", len(calls))
	}
}

// TestShortSessionCaseStillAssertsOrder guards the other side: sequence is a real
// behavioral property when it is short enough for one plan to express, so the
// granularity cap must not quietly disable ordering everywhere.
func TestShortSessionCaseStillAssertsOrder(t *testing.T) {
	rec := buildSkillValidationCaseFromSession(backfillRequest(), generation.SessionContext{
		Key:     "client:main:abc",
		AllText: "프로젝트 현황 알려줘",
		ToolActivities: []generation.ToolActivity{
			{Name: "read", Input: "checklist"},
			{Name: "exec", Input: "topsolar.py dashboard"},
		},
	})
	if len(rec.Replay.ExpectedToolCalls) != 2 {
		t.Fatalf("expected both calls kept, got %+v", rec.Replay.ExpectedToolCalls)
	}
	if !rec.Replay.RequireOrder {
		t.Error("a two-call trace is short enough to assert ordering")
	}
	names := make([]string, 0, 2)
	for _, call := range rec.Replay.ExpectedToolCalls {
		names = append(names, call.Name)
	}
	if strings.Join(names, ",") != "read,exec" {
		t.Errorf("order must follow the trace, got %v", names)
	}
}
