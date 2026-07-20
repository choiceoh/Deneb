package wiki

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newRecallHitsStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

// injectEvents builds bare path-only inject events — the minimal shape;
// retrieval context stays zero-valued, matching legacy ledger lines.
func injectEvents(paths ...string) []RecallEvent {
	events := make([]RecallEvent, 0, len(paths))
	for _, p := range paths {
		events = append(events, RecallEvent{Path: p, Event: RecallEventInject})
	}
	return events
}

func TestRecordRecallEvents_CollapsesDuplicatesWithinCallIgnoresEmptyPaths(t *testing.T) {
	store := newRecallHitsStore(t)

	// Two turns surface 프로젝트/A three times total, 인물/B once; a duplicate
	// within one call collapses to a single utility event.
	if err := store.RecordRecallEvents(injectEvents("프로젝트/A.md", "프로젝트/A.md", "인물/B.md")); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := store.RecordRecallEvents(injectEvents("프로젝트/A.md")); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if err := store.RecordRecallEvents(injectEvents("", "  ")); err != nil { // empties ignored
		t.Fatalf("record 3: %v", err)
	}

	counts := store.recallHitCounts(time.Now().Add(-time.Hour))
	if counts["프로젝트/A.md"] != 2 {
		t.Errorf("프로젝트/A.md count = %d, want 2 (dup within a call collapses)", counts["프로젝트/A.md"])
	}
	if counts["인물/B.md"] != 1 {
		t.Errorf("인물/B.md count = %d, want 1", counts["인물/B.md"])
	}
	if _, ok := counts[""]; ok {
		t.Errorf("empty path recorded")
	}
}

func TestRecordRecallEvents_DistinctKindsForOnePathAllRecorded(t *testing.T) {
	store := newRecallHitsStore(t)

	// One turn: the page is injected, then opened by the model, then cited in
	// the answer — three distinct utility events, not one.
	err := store.RecordRecallEvents([]RecallEvent{
		{Path: "프로젝트/A.md", Event: RecallEventInject, Query: "당진 현장", Session: "client:main"},
		{Path: "프로젝트/A.md", Event: RecallEventRead, Session: "client:main"},
		{Path: "프로젝트/A.md", Event: RecallEventCite, Session: "client:main"},
		{Path: "프로젝트/A.md", Event: RecallEventCite}, // dup (path, event) collapses
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}

	usage := store.RecallUsageScoreCounts(time.Now())
	got := usage["프로젝트/A.md"]
	if got.Injects != 1 || got.Reads != 1 || got.Cites != 1 {
		t.Errorf("usage = %+v, want 1/1/1", got)
	}
	if !got.Used() {
		t.Errorf("Used() = false with read+cite present")
	}
	// recallHitCounts sees total ledger activity across kinds.
	if n := store.recallHitCounts(time.Now().Add(-time.Hour))["프로젝트/A.md"]; n != 3 {
		t.Errorf("total count = %d, want 3 (one per kind)", n)
	}
}

func TestRecordRecallEvents_PersistsRetrievalContextAlongsideLegacyLines(t *testing.T) {
	store := newRecallHitsStore(t)

	// A pre-context line written by an older build: path+at only.
	legacy := fmt.Sprintf(`{"path":"프로젝트/legacy.md","at":%d}`, time.Now().UnixMilli())
	ledger := filepath.Join(store.dir, recallHitsFile)
	if err := os.WriteFile(ledger, []byte(legacy+"\n"), 0o644); err != nil {
		t.Fatalf("seed legacy line: %v", err)
	}

	longQuery := strings.Repeat("탑", recallHitQueryMaxRunes+40)
	if err := store.RecordRecallEvents([]RecallEvent{
		{Path: "프로젝트/탑솔라.md", Query: "탑솔라 계약", Rank: 2, Score: 0.91236789},
		{Path: "인물/김.md", Query: longQuery, Rank: 5, Score: 1.5},
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	store.recallMu.Lock()
	hits := store.readRecallHitsLocked()
	store.recallMu.Unlock()
	if len(hits) != 3 {
		t.Fatalf("hits = %d, want 3 (legacy + 2 contextual)", len(hits))
	}
	if hits[0].Path != "프로젝트/legacy.md" || hits[0].Query != "" || hits[0].Rank != 0 || hits[0].Score != 0 {
		t.Errorf("legacy line must parse with zero-valued context, got %+v", hits[0])
	}
	if hits[0].Event != RecallEventInject {
		t.Errorf("legacy line event = %q, want inject (reader normalizes)", hits[0].Event)
	}
	got := hits[1]
	if got.Query != "탑솔라 계약" || got.Rank != 2 || got.Event != RecallEventInject {
		t.Errorf("context not persisted: %+v", got)
	}
	if got.Score != 0.9124 {
		t.Errorf("score = %v, want 0.9124 (rounded to 4 decimals)", got.Score)
	}
	if n := len([]rune(hits[2].Query)); n != recallHitQueryMaxRunes {
		t.Errorf("oversized query clipped to %d runes, want %d", n, recallHitQueryMaxRunes)
	}

	// Aggregate readers count legacy and contextual lines alike, and the usage
	// view sees all three as exposure (no observed use recorded).
	counts := store.recallHitCounts(time.Time{})
	usage := store.RecallUsageScoreCounts(time.Now())
	for _, p := range []string{"프로젝트/legacy.md", "프로젝트/탑솔라.md", "인물/김.md"} {
		if counts[p] != 1 {
			t.Errorf("counts[%s] = %d, want 1", p, counts[p])
		}
		if u := usage[p]; u.Injects != 1 || u.Used() {
			t.Errorf("usage[%s] = %+v, want inject-only", p, u)
		}
	}
}

func TestRecallHitCounts_FutureSinceRejectsAllHits(t *testing.T) {
	store := newRecallHitsStore(t)
	if err := store.RecordRecallEvents(injectEvents("프로젝트/old.md")); err != nil {
		t.Fatalf("record: %v", err)
	}
	// A window starting in the future excludes the just-written hit.
	counts := store.recallHitCounts(time.Now().Add(time.Hour))
	if len(counts) != 0 {
		t.Errorf("expected no hits inside future window, got %v", counts)
	}
}

func TestCompactRecallHits_NoopWhenFreshDropsAfterRetentionElapses(t *testing.T) {
	store := newRecallHitsStore(t)
	if err := store.RecordRecallEvents(injectEvents("프로젝트/fresh.md")); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Nothing is older than retention now → no-op.
	dropped, err := store.compactRecallHits(time.Now())
	if err != nil {
		t.Fatalf("compact no-op: %v", err)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 on fresh ledger", dropped)
	}

	// Advance the clock past retention: the fresh line is now aged out.
	future := time.Now().Add(recallHitRetention + 24*time.Hour)
	dropped, err = store.compactRecallHits(future)
	if err != nil {
		t.Fatalf("compact aged: %v", err)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 after retention elapsed", dropped)
	}
	if got := store.recallHitCounts(time.Time{}); len(got) != 0 {
		t.Errorf("ledger not emptied after compaction: %v", got)
	}
}

func TestCompactRecallHits_PreservesContextAndUsageFields(t *testing.T) {
	store := newRecallHitsStore(t)

	// An aged-out legacy line plus fresh contextual/usage lines: compaction
	// must drop the former and rewrite the latter without losing any field.
	now := time.Now()
	aged := fmt.Sprintf(`{"path":"프로젝트/aged.md","at":%d}`,
		now.Add(-recallHitRetention-24*time.Hour).UnixMilli())
	ledger := filepath.Join(store.dir, recallHitsFile)
	if err := os.WriteFile(ledger, []byte(aged+"\n"), 0o644); err != nil {
		t.Fatalf("seed aged line: %v", err)
	}
	if err := store.RecordRecallEvents([]RecallEvent{
		{Path: "프로젝트/탑솔라.md", Query: "탑솔라 계약", Rank: 1, Score: 0.9, Session: "client:main"},
		{Path: "프로젝트/탑솔라.md", Event: RecallEventCite, Session: "client:main"},
	}); err != nil {
		t.Fatalf("record: %v", err)
	}

	dropped, err := store.compactRecallHits(now)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 (aged line only)", dropped)
	}
	store.recallMu.Lock()
	hits := store.readRecallHitsLocked()
	store.recallMu.Unlock()
	if len(hits) != 2 {
		t.Fatalf("hits after compact = %d, want 2", len(hits))
	}
	inject := hits[0]
	if inject.Path != "프로젝트/탑솔라.md" || inject.Query != "탑솔라 계약" || inject.Rank != 1 ||
		inject.Score != 0.9 || inject.Session != "client:main" || inject.Event != RecallEventInject {
		t.Errorf("compaction lost inject context: %+v", inject)
	}
	if cite := hits[1]; cite.Event != RecallEventCite || cite.Session != "client:main" {
		t.Errorf("compaction lost cite fields: %+v", cite)
	}
}

func TestRecallHitCounts_MissingLedger(t *testing.T) {
	store := newRecallHitsStore(t)
	if got := store.recallHitCounts(time.Time{}); len(got) != 0 {
		t.Errorf("missing ledger should yield empty counts, got %v", got)
	}
	if dropped, err := store.compactRecallHits(time.Now()); err != nil || dropped != 0 {
		t.Errorf("compact on missing ledger: dropped=%d err=%v", dropped, err)
	}
}
