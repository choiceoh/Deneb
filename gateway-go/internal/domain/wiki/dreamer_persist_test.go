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
