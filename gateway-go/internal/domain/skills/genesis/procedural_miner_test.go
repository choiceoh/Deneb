package genesis

import (
	"strings"
	"testing"
)

func trace(session string, tools ...string) ProceduralTraceRecord {
	return ProceduralTraceRecord{SessionKey: session, Tools: tools}
}

func findSeq(results []RecurringToolSequence, tools ...string) *RecurringToolSequence {
	want := strings.Join(tools, "\x00")
	for i := range results {
		if strings.Join(results[i].Tools, "\x00") == want {
			return &results[i]
		}
	}
	return nil
}

func TestMineRecurringToolSequences_BasicRecurrence(t *testing.T) {
	traces := []ProceduralTraceRecord{
		trace("s1", "calendar", "people", "mail"),
		trace("s2", "calendar", "people", "mail"),
		trace("s3", "calendar", "people", "mail"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 5})

	seq := findSeq(got, "calendar", "people", "mail")
	if seq == nil {
		t.Fatalf("expected the shared 3-step sequence to be mined; got %+v", got)
	}
	if seq.Sessions != 3 {
		t.Fatalf("expected Sessions=3, got %d", seq.Sessions)
	}
	// Consolidation: the 2-grams share the same 3 sessions and are subsumed by
	// the full 3-gram, so only the maximal sequence survives.
	if s := findSeq(got, "calendar", "people"); s != nil {
		t.Fatalf("expected [calendar people] to be consolidated away, got %+v", s)
	}
}

func TestMineRecurringToolSequences_BelowThresholdDropped(t *testing.T) {
	traces := []ProceduralTraceRecord{
		trace("s1", "read", "exec"),
		trace("s2", "read", "exec"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 5})
	if len(got) != 0 {
		t.Fatalf("a sequence in only 2 sessions must not clear MinSessions=3, got %+v", got)
	}
}

func TestMineRecurringToolSequences_DistinctSessionsNotOccurrences(t *testing.T) {
	// One session repeats [a b] three times; two other sessions have it once.
	// That is 3 distinct sessions but 5 occurrences — the threshold keys on
	// distinct sessions so a single loopy session cannot fake recurrence.
	traces := []ProceduralTraceRecord{
		trace("loopy", "a", "b", "a", "b", "a", "b"),
		trace("s2", "a", "b"),
		trace("s3", "a", "b"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 2})
	seq := findSeq(got, "a", "b")
	if seq == nil {
		t.Fatalf("expected [a b] mined, got %+v", got)
	}
	if seq.Sessions != 3 {
		t.Fatalf("expected Sessions=3 (distinct), got %d", seq.Sessions)
	}
	if seq.Occurrences != 5 {
		t.Fatalf("expected Occurrences=5 (3 in loopy + 1 + 1), got %d", seq.Occurrences)
	}
}

func TestMineRecurringToolSequences_OneLoopySessionIsNotRecurring(t *testing.T) {
	// The same n-gram repeated within a SINGLE session is not "recurring" — it is
	// one session looping. With MinSessions=3 and only one source session it must
	// not be mined, no matter how many times it occurs.
	traces := []ProceduralTraceRecord{
		trace("only", "a", "b", "a", "b", "a", "b", "a", "b"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 2})
	if len(got) != 0 {
		t.Fatalf("a single looping session must not count as recurrence, got %+v", got)
	}
}

func TestMineRecurringToolSequences_KeepsBroaderShorterSequence(t *testing.T) {
	// [search read] recurs in 5 sessions; the longer [search read write] only in
	// 3. The 3-gram is the maximal shape for those 3, but the 2-gram is a
	// genuinely MORE GENERAL procedure (broader coverage) and must survive
	// consolidation rather than be folded into the longer one.
	traces := []ProceduralTraceRecord{
		trace("a", "search", "read", "write"),
		trace("b", "search", "read", "write"),
		trace("c", "search", "read", "write"),
		trace("d", "search", "read", "delete"),
		trace("e", "search", "read", "delete"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 3})

	short := findSeq(got, "search", "read")
	if short == nil || short.Sessions != 5 {
		t.Fatalf("expected [search read] kept with Sessions=5, got %+v", got)
	}
	long := findSeq(got, "search", "read", "write")
	if long == nil || long.Sessions != 3 {
		t.Fatalf("expected [search read write] kept with Sessions=3, got %+v", got)
	}
	// [read write] (3 sessions) IS subsumed by [search read write] (same 3),
	// so it must be consolidated away.
	if s := findSeq(got, "read", "write"); s != nil {
		t.Fatalf("expected [read write] consolidated away, got %+v", s)
	}
	// [search read delete] recurs in only 2 sessions → below threshold.
	if s := findSeq(got, "search", "read", "delete"); s != nil {
		t.Fatalf("expected [search read delete] dropped (2 sessions < 3), got %+v", s)
	}
}

func TestMineRecurringToolSequences_RankingAndScore(t *testing.T) {
	traces := []ProceduralTraceRecord{
		trace("a", "search", "read", "write"),
		trace("b", "search", "read", "write"),
		trace("c", "search", "read", "write"),
		trace("d", "search", "read", "delete"),
		trace("e", "search", "read", "delete"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 3})
	if len(got) < 2 {
		t.Fatalf("expected at least two candidates, got %+v", got)
	}
	// Score = sessions * len. [search read]=5*2=10 outranks [search read write]=3*3=9.
	if got[0].Score != 10 {
		t.Fatalf("expected top score 10 ([search read]), got %v (%+v)", got[0].Score, got)
	}
	if !(got[0].Score >= got[1].Score) {
		t.Fatalf("results must be sorted by score desc, got %+v", got)
	}
}

func TestMineRecurringToolSequences_TopK(t *testing.T) {
	traces := []ProceduralTraceRecord{
		trace("a", "t1", "t2", "t3", "t4", "t5"),
		trace("b", "t1", "t2", "t3", "t4", "t5"),
		trace("c", "t1", "t2", "t3", "t4", "t5"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 4, TopK: 2})
	if len(got) > 2 {
		t.Fatalf("TopK=2 must cap results, got %d (%+v)", len(got), got)
	}
}

func TestMineRecurringToolSequences_NormalizesNamesAndDropsEmpties(t *testing.T) {
	traces := []ProceduralTraceRecord{
		trace("a", " Read ", "EXEC", ""),
		trace("b", "read", "exec"),
		trace("c", "read", "exec"),
	}
	got := MineRecurringToolSequences(traces, ProceduralMineOptions{MinSessions: 3, MinLen: 2, MaxLen: 2})
	if findSeq(got, "read", "exec") == nil {
		t.Fatalf("expected case/space-normalized [read exec] mined across 3 sessions, got %+v", got)
	}
}

func TestMineRecurringToolSequences_EmptyAndDegenerate(t *testing.T) {
	if got := MineRecurringToolSequences(nil, DefaultProceduralMineOptions()); len(got) != 0 {
		t.Fatalf("nil corpus must yield no candidates, got %+v", got)
	}
	// MinLen below 2 is clamped (a single tool is not a procedure), so a corpus
	// of single-tool traces yields nothing.
	single := []ProceduralTraceRecord{trace("a", "read"), trace("b", "read"), trace("c", "read")}
	if got := MineRecurringToolSequences(single, ProceduralMineOptions{MinSessions: 3, MinLen: 1, MaxLen: 1}); len(got) != 0 {
		t.Fatalf("single-tool traces must yield no procedure, got %+v", got)
	}
	// A blank session key is skipped.
	blank := []ProceduralTraceRecord{trace("", "a", "b"), trace("", "a", "b"), trace("", "a", "b")}
	if got := MineRecurringToolSequences(blank, ProceduralMineOptions{MinSessions: 2, MinLen: 2, MaxLen: 2}); len(got) != 0 {
		t.Fatalf("blank session keys must not aggregate into recurrence, got %+v", got)
	}
}
