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

// recallPathHits builds bare path-only records — the minimal shape; retrieval
// context stays zero-valued, matching legacy ledger lines.
func recallPathHits(paths ...string) []RecallHitRecord {
	out := make([]RecallHitRecord, 0, len(paths))
	for _, p := range paths {
		out = append(out, RecallHitRecord{Path: p})
	}
	return out
}

func TestRecordRecallHits_CollapsesDuplicatesWithinCallIgnoresEmptyPaths(t *testing.T) {
	store := newRecallHitsStore(t)

	// Two turns surface 프로젝트/A three times total, 인물/B once; a duplicate
	// within one call collapses to a single utility event.
	if err := store.RecordRecallHits(recallPathHits("프로젝트/A.md", "프로젝트/A.md", "인물/B.md")); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := store.RecordRecallHits(recallPathHits("프로젝트/A.md")); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if err := store.RecordRecallHits(recallPathHits("", "  ")); err != nil { // empties ignored
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

func TestRecallHitCounts_FutureSinceRejectsAllHits(t *testing.T) {
	store := newRecallHitsStore(t)
	if err := store.RecordRecallHits(recallPathHits("프로젝트/old.md")); err != nil {
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
	if err := store.RecordRecallHits(recallPathHits("프로젝트/fresh.md")); err != nil {
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

func TestRecallHitCounts_MissingLedger(t *testing.T) {
	store := newRecallHitsStore(t)
	if got := store.recallHitCounts(time.Time{}); len(got) != 0 {
		t.Errorf("missing ledger should yield empty counts, got %v", got)
	}
	if dropped, err := store.compactRecallHits(time.Now()); err != nil || dropped != 0 {
		t.Errorf("compact on missing ledger: dropped=%d err=%v", dropped, err)
	}
}

func TestRecordRecallHits_PersistsRetrievalContextAlongsideLegacyLines(t *testing.T) {
	store := newRecallHitsStore(t)

	// A pre-context line written by an older build: path+at only.
	legacy := fmt.Sprintf(`{"path":"프로젝트/legacy.md","at":%d}`, time.Now().UnixMilli())
	ledger := filepath.Join(store.dir, recallHitsFile)
	if err := os.WriteFile(ledger, []byte(legacy+"\n"), 0o644); err != nil {
		t.Fatalf("seed legacy line: %v", err)
	}

	longQuery := strings.Repeat("탑", recallHitQueryMaxRunes+40)
	if err := store.RecordRecallHits([]RecallHitRecord{
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
	got := hits[1]
	if got.Query != "탑솔라 계약" || got.Rank != 2 {
		t.Errorf("context not persisted: %+v", got)
	}
	if got.Score != 0.9124 {
		t.Errorf("score = %v, want 0.9124 (rounded to 4 decimals)", got.Score)
	}
	if n := len([]rune(hits[2].Query)); n != recallHitQueryMaxRunes {
		t.Errorf("oversized query clipped to %d runes, want %d", n, recallHitQueryMaxRunes)
	}

	// Aggregate readers count legacy and contextual lines alike.
	counts := store.recallHitCounts(time.Time{})
	for _, p := range []string{"프로젝트/legacy.md", "프로젝트/탑솔라.md", "인물/김.md"} {
		if counts[p] != 1 {
			t.Errorf("counts[%s] = %d, want 1", p, counts[p])
		}
	}
}

func TestCompactRecallHits_PreservesRetrievalContext(t *testing.T) {
	store := newRecallHitsStore(t)

	// An aged-out legacy line plus a fresh contextual line: compaction must
	// drop the former and rewrite the latter without losing its context.
	now := time.Now()
	aged := fmt.Sprintf(`{"path":"프로젝트/aged.md","at":%d}`,
		now.Add(-recallHitRetention-24*time.Hour).UnixMilli())
	ledger := filepath.Join(store.dir, recallHitsFile)
	if err := os.WriteFile(ledger, []byte(aged+"\n"), 0o644); err != nil {
		t.Fatalf("seed aged line: %v", err)
	}
	if err := store.RecordRecallHits([]RecallHitRecord{
		{Path: "프로젝트/탑솔라.md", Query: "탑솔라 계약", Rank: 1, Score: 0.9},
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
	if len(hits) != 1 {
		t.Fatalf("hits after compact = %d, want 1", len(hits))
	}
	if h := hits[0]; h.Path != "프로젝트/탑솔라.md" || h.Query != "탑솔라 계약" || h.Rank != 1 || h.Score != 0.9 {
		t.Errorf("compaction lost retrieval context: %+v", h)
	}
}
