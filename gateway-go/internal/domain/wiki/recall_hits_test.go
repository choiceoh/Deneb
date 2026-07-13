package wiki

import (
	"path/filepath"
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

func TestRecordRecallHits_CountsAndWindow(t *testing.T) {
	store := newRecallHitsStore(t)

	// Two turns surface 프로젝트/A three times total, 인물/B once; a duplicate
	// within one call collapses to a single utility event.
	if err := store.RecordRecallHits([]string{"프로젝트/A.md", "프로젝트/A.md", "인물/B.md"}); err != nil {
		t.Fatalf("record 1: %v", err)
	}
	if err := store.RecordRecallHits([]string{"프로젝트/A.md"}); err != nil {
		t.Fatalf("record 2: %v", err)
	}
	if err := store.RecordRecallHits([]string{"", "  "}); err != nil { // empties ignored
		t.Fatalf("record 3: %v", err)
	}

	counts := store.RecallHitCounts(time.Now().Add(-time.Hour))
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

func TestRecallHitCounts_ExcludesBeforeSince(t *testing.T) {
	store := newRecallHitsStore(t)
	if err := store.RecordRecallHits([]string{"프로젝트/old.md"}); err != nil {
		t.Fatalf("record: %v", err)
	}
	// A window starting in the future excludes the just-written hit.
	counts := store.RecallHitCounts(time.Now().Add(time.Hour))
	if len(counts) != 0 {
		t.Errorf("expected no hits inside future window, got %v", counts)
	}
}

func TestCompactRecallHits_DropsAgedLines(t *testing.T) {
	store := newRecallHitsStore(t)
	if err := store.RecordRecallHits([]string{"프로젝트/fresh.md"}); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Nothing is older than retention now → no-op.
	dropped, err := store.CompactRecallHits(time.Now())
	if err != nil {
		t.Fatalf("compact no-op: %v", err)
	}
	if dropped != 0 {
		t.Errorf("dropped = %d, want 0 on fresh ledger", dropped)
	}

	// Advance the clock past retention: the fresh line is now aged out.
	future := time.Now().Add(recallHitRetention + 24*time.Hour)
	dropped, err = store.CompactRecallHits(future)
	if err != nil {
		t.Fatalf("compact aged: %v", err)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, want 1 after retention elapsed", dropped)
	}
	if got := store.RecallHitCounts(time.Time{}); len(got) != 0 {
		t.Errorf("ledger not emptied after compaction: %v", got)
	}
}

func TestRecallHitCounts_MissingLedger(t *testing.T) {
	store := newRecallHitsStore(t)
	if got := store.RecallHitCounts(time.Time{}); len(got) != 0 {
		t.Errorf("missing ledger should yield empty counts, got %v", got)
	}
	if dropped, err := store.CompactRecallHits(time.Now()); err != nil || dropped != 0 {
		t.Errorf("compact on missing ledger: dropped=%d err=%v", dropped, err)
	}
}
