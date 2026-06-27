package genesis

import (
	"strings"
	"testing"
)

func TestRecordAndRecentProceduralTraces(t *testing.T) {
	tr := newTestTracker(t)

	if err := tr.RecordProceduralTrace("s1", []string{"calendar", "people"}); err != nil {
		t.Fatalf("record s1: %v", err)
	}
	if err := tr.RecordProceduralTrace("s2", []string{"mail", "wiki"}); err != nil {
		t.Fatalf("record s2: %v", err)
	}

	got, err := tr.RecentProceduralTraces(10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 traces, got %d (%+v)", len(got), got)
	}
	// Newest session first.
	if got[0].SessionKey != "s2" || got[1].SessionKey != "s1" {
		t.Fatalf("expected newest-first [s2 s1], got [%s %s]", got[0].SessionKey, got[1].SessionKey)
	}
	if strings.Join(got[1].Tools, ",") != "calendar,people" {
		t.Fatalf("s1 tools not preserved: %+v", got[1].Tools)
	}
}

func TestRecordProceduralTrace_DedupeKeepsLatest(t *testing.T) {
	tr := newTestTracker(t)

	// Same session recorded as it grew (the Nudger fires mid-session repeatedly):
	// the final, fullest sequence must win on read.
	if err := tr.RecordProceduralTrace("s", []string{"a", "b"}); err != nil {
		t.Fatalf("record short: %v", err)
	}
	if err := tr.RecordProceduralTrace("s", []string{"a", "b", "c", "d"}); err != nil {
		t.Fatalf("record long: %v", err)
	}

	got, err := tr.RecentProceduralTraces(10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the session deduped to 1 row, got %d (%+v)", len(got), got)
	}
	if strings.Join(got[0].Tools, ",") != "a,b,c,d" {
		t.Fatalf("expected latest (fullest) sequence, got %+v", got[0].Tools)
	}
}

func TestRecordProceduralTrace_DropsTooShortAndEmpty(t *testing.T) {
	tr := newTestTracker(t)

	if err := tr.RecordProceduralTrace("s1", []string{"only-one"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := tr.RecordProceduralTrace("", []string{"a", "b"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := tr.RecordProceduralTrace("s2", []string{"  ", ""}); err != nil {
		t.Fatalf("record: %v", err)
	}

	got, err := tr.RecentProceduralTraces(10)
	if err != nil {
		t.Fatalf("recent: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("too-short / empty-key / blank-tool records must be dropped, got %+v", got)
	}
}

func TestMineProceduralSkillCandidates_EndToEnd(t *testing.T) {
	tr := newTestTracker(t)

	for _, s := range []string{"s1", "s2", "s3"} {
		if err := tr.RecordProceduralTrace(s, []string{"calendar", "people", "mail"}); err != nil {
			t.Fatalf("record %s: %v", s, err)
		}
	}
	// A one-off session that must not reach the recurrence threshold.
	if err := tr.RecordProceduralTrace("odd", []string{"git", "exec"}); err != nil {
		t.Fatalf("record odd: %v", err)
	}

	got, err := tr.MineProceduralSkillCandidates(ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 5})
	if err != nil {
		t.Fatalf("mine: %v", err)
	}
	if findSeq(got, "calendar", "people", "mail") == nil {
		t.Fatalf("expected the recurring 3-step sequence mined from the corpus, got %+v", got)
	}
	if findSeq(got, "git", "exec") != nil {
		t.Fatalf("one-off [git exec] must not be mined, got %+v", got)
	}
}
