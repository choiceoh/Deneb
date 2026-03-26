package autoreply

import (
	"fmt"
	"testing"
)

func ptr64(v int64) *int64 { return &v }

func TestResolveSubagentLabel(t *testing.T) {
	r := &SubagentRunRecord{Label: " my-task "}
	if got := ResolveSubagentLabel(r, ""); got != "my-task" {
		t.Errorf("label = %q", got)
	}

	r = &SubagentRunRecord{Task: "do stuff"}
	if got := ResolveSubagentLabel(r, ""); got != "do stuff" {
		t.Errorf("task fallback = %q", got)
	}

	r = &SubagentRunRecord{}
	if got := ResolveSubagentLabel(r, "default"); got != "default" {
		t.Errorf("fallback = %q", got)
	}
}

func TestFormatRunStatus(t *testing.T) {
	// Running.
	r := &SubagentRunRecord{}
	if got := FormatRunStatus(r); got != "running" {
		t.Errorf("running = %q", got)
	}

	// Ended with ok outcome.
	r = &SubagentRunRecord{EndedAt: ptr64(100), Outcome: &SubagentRunOutcome{Status: "ok"}}
	if got := FormatRunStatus(r); got != "done" {
		t.Errorf("ok = %q", got)
	}

	// Ended with error.
	r = &SubagentRunRecord{EndedAt: ptr64(100), Outcome: &SubagentRunOutcome{Status: "error"}}
	if got := FormatRunStatus(r); got != "error" {
		t.Errorf("error = %q", got)
	}

	// Ended with no outcome.
	r = &SubagentRunRecord{EndedAt: ptr64(100)}
	if got := FormatRunStatus(r); got != "done" {
		t.Errorf("no outcome = %q", got)
	}
}

func TestSortSubagentRuns(t *testing.T) {
	runs := []*SubagentRunRecord{
		{RunID: "a", CreatedAt: 100},
		{RunID: "b", StartedAt: ptr64(300)},
		{RunID: "c", CreatedAt: 200},
	}
	sorted := SortSubagentRuns(runs)
	if sorted[0].RunID != "b" || sorted[1].RunID != "c" || sorted[2].RunID != "a" {
		t.Errorf("sort order: %s %s %s", sorted[0].RunID, sorted[1].RunID, sorted[2].RunID)
	}
	// Original unchanged.
	if runs[0].RunID != "a" {
		t.Error("original mutated")
	}
}

func TestResolveSubagentTargetFromRuns(t *testing.T) {
	now := int64(1000000)
	runs := []*SubagentRunRecord{
		{RunID: "run-abc", Label: "worker", ChildSessionKey: "sess:1", CreatedAt: now - 100, StartedAt: ptr64(now - 50)},
		{RunID: "run-def", Label: "builder", ChildSessionKey: "sess:2", CreatedAt: now - 200, StartedAt: ptr64(now - 150), EndedAt: ptr64(now - 10)},
	}

	errors := SubagentTargetErrors{
		MissingTarget:  "missing",
		InvalidIndex:   func(v string) string { return fmt.Sprintf("invalid: %s", v) },
		UnknownSession: func(v string) string { return fmt.Sprintf("unknown session: %s", v) },
		AmbiguousLabel: func(v string) string { return fmt.Sprintf("ambiguous: %s", v) },
		AmbiguousLabelPfx: func(v string) string { return fmt.Sprintf("ambiguous pfx: %s", v) },
		AmbiguousRunIDPfx: func(v string) string { return fmt.Sprintf("ambiguous run: %s", v) },
		UnknownTarget:  func(v string) string { return fmt.Sprintf("unknown: %s", v) },
	}
	labelFn := func(e *SubagentRunRecord) string { return ResolveSubagentLabel(e, "") }

	// Empty token.
	r := ResolveSubagentTargetFromRuns(runs, "", 60, labelFn, nil, errors)
	if r.Error != "missing" {
		t.Errorf("empty: %+v", r)
	}

	// "last".
	r = ResolveSubagentTargetFromRuns(runs, "last", 60, labelFn, nil, errors)
	if r.Entry == nil || r.Entry.RunID != "run-abc" {
		t.Errorf("last: %+v", r)
	}

	// By session key.
	r = ResolveSubagentTargetFromRuns(runs, "sess:2", 60, labelFn, nil, errors)
	if r.Entry == nil || r.Entry.RunID != "run-def" {
		t.Errorf("session key: %+v", r)
	}

	// By exact label.
	r = ResolveSubagentTargetFromRuns(runs, "worker", 60, labelFn, nil, errors)
	if r.Entry == nil || r.Entry.RunID != "run-abc" {
		t.Errorf("exact label: %+v", r)
	}

	// By run ID prefix.
	r = ResolveSubagentTargetFromRuns(runs, "run-d", 60, labelFn, nil, errors)
	if r.Entry == nil || r.Entry.RunID != "run-def" {
		t.Errorf("run ID prefix: %+v", r)
	}

	// Ambiguous run ID prefix.
	r = ResolveSubagentTargetFromRuns(runs, "run-", 60, labelFn, nil, errors)
	if r.Error == "" {
		t.Error("expected ambiguous run ID error")
	}

	// Unknown target.
	r = ResolveSubagentTargetFromRuns(runs, "nonexistent", 60, labelFn, nil, errors)
	if r.Error == "" {
		t.Error("expected unknown target error")
	}
}
