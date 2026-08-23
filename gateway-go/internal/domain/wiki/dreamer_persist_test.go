package wiki

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/testutil"
)

// TestWikiDreamerPersistsLastDreamAcrossRestart verifies the fix for dreaming
// being dead for ~26 days: lastDream must survive a "restart" (a fresh
// WikiDreamer) via persisted state, so the 8h time-trigger actually fires
// instead of the in-memory lastDream resetting to zero on every boot.
func TestWikiDreamerPersistsLastDreamAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))

	// First dreamer with no persisted state seeds lastDream=now (non-zero) so
	// the interval starts counting instead of staying zero forever.
	wd1 := NewWikiDreamer(store, nil, "", Config{}, logger)
	if wd1.lastDream.IsZero() {
		t.Fatal("expected lastDream to be seeded (non-zero) on first run")
	}

	// Record a dream 9h ago on disk.
	old := time.Now().Add(-9 * time.Hour)
	state := wd1.loadDiaryProcessState()
	state.LastDreamMs = old.UnixMilli()
	if err := wd1.saveDiaryProcessState(state); err != nil {
		t.Fatalf("save: %v", err)
	}

	// A fresh dreamer (= gateway restart) must restore lastDream from disk,
	// not reset it to zero.
	wd2 := NewWikiDreamer(store, nil, "", Config{}, logger)
	if d := wd2.lastDream.Sub(old); d > time.Second || d < -time.Second {
		t.Fatalf("expected lastDream restored to ~%v, got %v", old, wd2.lastDream)
	}

	// With lastDream 9h ago (> 8h interval) the time-trigger must fire — the
	// exact path that was permanently blocked before persistence.
	if !wd2.ShouldDream(context.Background()) {
		t.Fatal("expected ShouldDream=true with lastDream 9h ago (time trigger)")
	}

	// resetCounters must persist the new lastDream so the next restart sees it.
	wd2.resetCounters()
	reloaded := wd2.loadDiaryProcessState()
	if reloaded.LastDreamMs == old.UnixMilli() || reloaded.LastDreamMs == 0 {
		t.Fatalf("expected resetCounters to persist a fresh lastDreamMs, got %d", reloaded.LastDreamMs)
	}
}

// TestShouldDream_PreferenceSignalAcceleratesCadenceWithFloor pins the 선호
// fast path: a pending preference signal (NotePreferenceSignal, fed by the
// chat diary recorder) drops the dream wait from 8h to
// wikiDreamPrefMinInterval, respects that floor so back-to-back preference
// turns can't thrash cycles, and is consumed by resetCounters at the end of a
// cycle.
func TestShouldDream_PreferenceSignalAcceleratesCadenceWithFloor(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, logger)
	ctx := context.Background()

	setLastDream := func(v time.Time) {
		wd.cmu.Lock()
		wd.lastDream = v
		wd.cmu.Unlock()
	}

	setLastDream(time.Now().Add(-wikiDreamPrefMinInterval - time.Minute))
	if wd.ShouldDream(ctx) {
		t.Fatal("without a preference signal, the sub-8h window must not trigger")
	}

	wd.NotePreferenceSignal()
	if !wd.ShouldDream(ctx) {
		t.Fatal("pending preference signal past the min interval must trigger")
	}

	// Within the floor the signal must NOT thrash dreams.
	setLastDream(time.Now())
	if wd.ShouldDream(ctx) {
		t.Fatal("preference signal must respect the min-interval floor")
	}

	// A completed cycle consumes the pending signal.
	wd.resetCounters()
	setLastDream(time.Now().Add(-wikiDreamPrefMinInterval - time.Minute))
	if wd.ShouldDream(ctx) {
		t.Fatal("resetCounters must clear the pending preference signal")
	}
}

func TestShouldDream_ProjectSignalAcceleratesCadenceWithFloor(t *testing.T) {
	dir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	store := testutil.Must(NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary")))
	t.Cleanup(func() { _ = store.Close() })
	wd := NewWikiDreamer(store, nil, "", Config{}, logger)
	ctx := context.Background()

	setLastDream := func(v time.Time) {
		wd.cmu.Lock()
		wd.lastDream = v
		wd.cmu.Unlock()
	}

	setLastDream(time.Now().Add(-wikiDreamPrefMinInterval - time.Minute))
	if wd.ShouldDream(ctx) {
		t.Fatal("without a project signal, the sub-8h window must not trigger")
	}

	wd.NoteProjectSignal()
	if !wd.ShouldDream(ctx) {
		t.Fatal("pending project signal past the min interval must trigger")
	}

	setLastDream(time.Now())
	if wd.ShouldDream(ctx) {
		t.Fatal("project signal must respect the min-interval floor")
	}

	wd.resetCounters()
	setLastDream(time.Now().Add(-wikiDreamPrefMinInterval - time.Minute))
	if wd.ShouldDream(ctx) {
		t.Fatal("resetCounters must clear the pending project signal")
	}
}

// TestRunDream_FailureBacksOffOneInterval guards the hot-loop fix: a cycle
// that cannot synthesize (nil/wedged LLM) must still advance lastDream, or
// ShouldDream stays true and the 30-min timer retries a doomed 10-minute
// cycle forever (observed in production 2026-06-11 with a wedged vLLM).
func TestRunDream_FailureBacksOffOneInterval(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(filepath.Join(dir, "wiki"), filepath.Join(dir, "diary"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.AppendDiary("백오프 테스트용 일지 항목"); err != nil {
		t.Fatal(err)
	}

	wd := &WikiDreamer{store: store, logger: slog.Default()}
	wd.lastDream = time.Now().Add(-24 * time.Hour) // stale → ShouldDream true
	if !wd.ShouldDream(context.Background()) {
		t.Fatal("precondition: ShouldDream must be true")
	}

	report, err := wd.RunDream(context.Background())
	if err != nil {
		t.Fatalf("RunDream: %v", err)
	}
	if len(report.PhaseErrors) == 0 {
		t.Fatal("expected a synthesis phase error with no LLM client")
	}
	if wd.ShouldDream(context.Background()) {
		t.Error("failed cycle must back off a full interval (lastDream advanced)")
	}
	// And the backoff must survive a restart.
	st := wd.loadDiaryProcessState()
	if st.LastDreamMs == 0 {
		t.Error("backoff lastDream not persisted")
	}
	// Unconsumed content must remain unconsumed (re-tried next interval).
	if st.MemoryConsumedThrough != "" {
		t.Errorf("failed cycle must not advance the memory high-water mark: %q", st.MemoryConsumedThrough)
	}
}
