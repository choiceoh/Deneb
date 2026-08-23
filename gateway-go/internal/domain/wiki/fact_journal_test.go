package wiki

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func upsertFactSeries(t *testing.T, store *Store, key string, count int) {
	t.Helper()
	base := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	for i := range count {
		if _, err := store.UpsertFact(FactInput{
			Subject: "self", Key: key, Value: fmt.Sprintf("값-%03d", i),
			Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
			At: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("upsert %d: %v", i, err)
		}
	}
}

// Rotation is what keeps startup bounded: the active segment is archived under
// the revision it ends at, and the permanent history stays on disk.
func TestFactJournalRotatesActiveSegmentAndKeepsHistory(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	store.factJournalRotateAt = 4
	upsertFactSeries(t, store, "communication.response_length", 9)
	revision := store.LatestFactRevision()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	archives, err := factJournalArchives(wikiDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(archives) != 2 {
		t.Fatalf("archives=%v", archives)
	}
	active, err := os.ReadFile(filepath.Join(wikiDir, factJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(strings.Fields(string(active))) == 0 {
		t.Fatalf("active segment should still hold the post-rotation tail: %q", active)
	}
	archived := 0
	for _, name := range archives {
		raw, err := os.ReadFile(filepath.Join(wikiDir, name))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
			var mutation FactMutation
			if err := json.Unmarshal([]byte(line), &mutation); err != nil {
				t.Fatalf("archived row is not a mutation: %v", err)
			}
			archived++
		}
	}
	if archived != 8 {
		t.Fatalf("archived rows=%d", archived)
	}

	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("reopen after rotation: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.LatestFactRevision(); got != revision {
		t.Fatalf("revision after reopen = %d, want %d", got, revision)
	}
	if got := reopened.ActiveFacts("self"); len(got) != 1 || got[0].Value != "값-008" {
		t.Fatalf("current fact after reopen = %+v", got)
	}
}

// The snapshot is a real replay start point, not just a watermark: startup must
// not re-apply what it already covers.
func TestFactPlaneStartupSeedsFromSnapshotWithoutReplayingArchives(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	store.factJournalRotateAt = 3
	upsertFactSeries(t, store, "communication.language", 7)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	archives, err := factJournalArchives(wikiDir)
	if err != nil || len(archives) == 0 {
		t.Fatalf("archives=%v err=%v", archives, err)
	}
	// Make every archive unreadable. A seeded start never opens them, so this
	// only fails if startup silently fell back to a full-history replay.
	for _, name := range archives {
		if err := os.WriteFile(filepath.Join(wikiDir, name), []byte("{ not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("seeded startup must not read archives: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.ActiveFacts("self"); len(got) != 1 || got[0].Value != "값-006" {
		t.Fatalf("seeded state = %+v", got)
	}
}

// Repair path: without a usable snapshot the plane is rebuilt from the full
// archived ledger rather than from whatever the active segment happens to hold.
func TestFactPlaneRebuildsFromArchivesWhenSnapshotIsUnusable(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	store.factJournalRotateAt = 3
	upsertFactSeries(t, store, "communication.format", 7)
	revision := store.LatestFactRevision()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(wikiDir, factStateFile)); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("rebuild from archives: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.LatestFactRevision(); got != revision {
		t.Fatalf("rebuilt revision = %d, want %d", got, revision)
	}
	if history := reopened.FactHistory("self", "communication.format"); len(history) != 7 {
		t.Fatalf("rebuilt history rows = %d", len(history))
	}
}

// Seeding must not hide a truncated ledger: state would sit at the snapshot
// revision no matter how much journal history disappeared.
func TestFactPlaneDetectsRolledBackJournalAfterRotation(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	store.factJournalRotateAt = 3
	upsertFactSeries(t, store, "communication.answer_first", 7)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	archives, err := factJournalArchives(wikiDir)
	if err != nil || len(archives) == 0 {
		t.Fatalf("archives=%v err=%v", archives, err)
	}
	// Drop the active tail AND every archive that could vouch for the watermark.
	if err := os.WriteFile(filepath.Join(wikiDir, factJournalFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range archives {
		if err := os.Remove(filepath.Join(wikiDir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewStore(wikiDir, diaryDir); err == nil || !strings.Contains(err.Error(), "snapshot watermark") {
		t.Fatalf("rolled-back journal error = %v", err)
	}
}

// A rotated-but-empty active segment is a normal state, not a rollback: the
// newest archive name is proof the missing revisions were durable.
func TestFactPlaneAcceptsEmptyActiveSegmentAfterRotation(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	store.factJournalRotateAt = 3
	upsertFactSeries(t, store, "communication.progress_updates", 6)
	revision := store.LatestFactRevision()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(wikiDir, factJournalFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "" {
		t.Fatalf("expected an empty active segment at this rotation boundary: %q", raw)
	}
	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("reopen with empty active segment: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.LatestFactRevision(); got != revision {
		t.Fatalf("revision = %d, want %d", got, revision)
	}
}

func TestFactJournalArchivesIgnoreUnrelatedFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		factJournalFile,
		factJournalArchiveName(12),
		factJournalArchiveName(3),
		".fact-mutations.backup.jsonl",
		".fact-mutations.jsonl.legacy",
		"unrelated.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := factJournalArchives(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{factJournalArchiveName(3), factJournalArchiveName(12)}
	if len(got) != len(want) {
		t.Fatalf("archives = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("archives = %v, want %v (revision order)", got, want)
		}
	}
	if rev := newestFactArchiveRevision(dir); rev != 12 {
		t.Fatalf("newest archive revision = %d", rev)
	}
}

// Rotation renames the active segment and then creates the next one. If that
// create fails, the archives still hold every revision — booting must recover
// instead of refusing over a file that is supposed to be empty.
func TestFactPlaneRecoversFromActiveSegmentLostAfterRotation(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	store.factJournalRotateAt = 3
	// Stop exactly on a rotation boundary: the newest archive ends at the same
	// revision as the snapshot, which is the state a failed create leaves behind.
	upsertFactSeries(t, store, "communication.language", 6)
	revision := store.LatestFactRevision()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	archives, err := factJournalArchives(wikiDir)
	if err != nil || len(archives) == 0 {
		t.Fatalf("archives=%v err=%v", archives, err)
	}
	if got := newestFactArchiveRevision(wikiDir); got != revision {
		t.Fatalf("newest archive revision = %d, want %d", got, revision)
	}
	if err := os.Remove(filepath.Join(wikiDir, factJournalFile)); err != nil {
		t.Fatal(err)
	}

	reopened, err := NewStore(wikiDir, diaryDir)
	if err != nil {
		t.Fatalf("reopen after a lost active segment: %v", err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if got := reopened.LatestFactRevision(); got != revision {
		t.Fatalf("revision = %d, want %d", got, revision)
	}
	if _, err := os.Stat(filepath.Join(wikiDir, factJournalFile)); err != nil {
		t.Fatalf("active segment was not recreated: %v", err)
	}
	// The recovered store must still accept writes.
	if _, err := reopened.UpsertFact(FactInput{
		Subject: "self", Key: "communication.language", Value: "복구 후 값",
		Kind: FactKindPreference, Authority: FactAuthorityDirectUser,
		At: time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("append after recovery: %v", err)
	}
}

// A missing active segment whose revisions no archive covers is real history
// loss — including the ordinary case of mutations appended after the last
// rotation — and must still fail closed.
func TestFactPlaneStillRefusesAMissingJournalWithoutArchives(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	upsertFactSeries(t, store, "communication.format", 2)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(wikiDir, factJournalFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(wikiDir, diaryDir); err == nil ||
		!strings.Contains(err.Error(), "fact journal missing for snapshot revision") {
		t.Fatalf("missing journal error = %v", err)
	}
}

// The dangerous variant: rotation happened, then more mutations landed in the
// new segment. Losing it loses those revisions, and no archive vouches for them.
func TestFactPlaneRefusesALostSegmentHoldingPostRotationRevisions(t *testing.T) {
	store, wikiDir, diaryDir := newFactTestStore(t)
	store.factJournalRotateAt = 3
	upsertFactSeries(t, store, "communication.answer_first", 7)
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(wikiDir, factJournalFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := NewStore(wikiDir, diaryDir); err == nil ||
		!strings.Contains(err.Error(), "fact journal missing for snapshot revision") {
		t.Fatalf("lost post-rotation tail error = %v", err)
	}
}
