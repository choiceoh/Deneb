package memory

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func tempStoreForDreaming(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "dreaming_test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// ─── IncrementTurn ─────────────────────────────────────────────────────────

func TestDreamingAdapter_IncrementTurn(t *testing.T) {
	s := tempStoreForDreaming(t)
	ctx := context.Background()
	da := NewDreamingAdapter(s, nil, nil, "", discardLogger())

	// Initial count should be 0 (empty string parses as 0).
	da.IncrementTurn(ctx)
	da.IncrementTurn(ctx)
	da.IncrementTurn(ctx)

	got, err := s.GetMeta(ctx, metaTurnCount)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if got != "3" {
		t.Errorf("turn count = %q, want %q", got, "3")
	}
}

// ─── ShouldDream ───────────────────────────────────────────────────────────

func TestDreamingAdapter_ShouldDream_underThreshold(t *testing.T) {
	s := tempStoreForDreaming(t)
	ctx := context.Background()
	da := NewDreamingAdapter(s, nil, nil, "", discardLogger())

	// Set turn count well below threshold.
	s.SetMeta(ctx, metaTurnCount, "10")
	// Set recent last-run time so time threshold is not met.
	s.SetMeta(ctx, metaLastDreaming, time.Now().UTC().Format(time.RFC3339))

	if da.ShouldDream(ctx) {
		t.Error("ShouldDream should be false when under both thresholds")
	}
}

func TestDreamingAdapter_ShouldDream_turnThreshold(t *testing.T) {
	s := tempStoreForDreaming(t)
	ctx := context.Background()
	da := NewDreamingAdapter(s, nil, nil, "", discardLogger())

	// Set turn count at threshold.
	s.SetMeta(ctx, metaTurnCount, "50")
	s.SetMeta(ctx, metaLastDreaming, time.Now().UTC().Format(time.RFC3339))

	if !da.ShouldDream(ctx) {
		t.Error("ShouldDream should be true when turn threshold reached")
	}
}

func TestDreamingAdapter_ShouldDream_timeThreshold(t *testing.T) {
	s := tempStoreForDreaming(t)
	ctx := context.Background()
	da := NewDreamingAdapter(s, nil, nil, "", discardLogger())

	// Low turn count.
	s.SetMeta(ctx, metaTurnCount, "5")
	// Last run was 9 hours ago (threshold is 8h).
	oldTime := time.Now().Add(-9 * time.Hour).UTC().Format(time.RFC3339)
	s.SetMeta(ctx, metaLastDreaming, oldTime)

	if !da.ShouldDream(ctx) {
		t.Error("ShouldDream should be true when time threshold exceeded")
	}
}

func TestDreamingAdapter_ShouldDream_firstRun(t *testing.T) {
	s := tempStoreForDreaming(t)
	ctx := context.Background()
	da := NewDreamingAdapter(s, nil, nil, "", discardLogger())

	// No prior turn count or last-run timestamp.
	// First call should initialize and return false.
	if da.ShouldDream(ctx) {
		t.Error("ShouldDream should be false on first invocation (initializes timestamp)")
	}

	// Verify the initial timestamp was set.
	got, err := s.GetMeta(ctx, metaLastDreaming)
	if err != nil {
		t.Fatalf("GetMeta: %v", err)
	}
	if got == "" {
		t.Error("expected initial timestamp to be set")
	}
	// Should be parseable.
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("timestamp %q not valid RFC3339: %v", got, err)
	}
}

// ─── ShouldDream edge cases ────────────────────────────────────────────────

func TestDreamingAdapter_ShouldDream_corruptedTimestamp(t *testing.T) {
	s := tempStoreForDreaming(t)
	ctx := context.Background()
	da := NewDreamingAdapter(s, nil, nil, "", discardLogger())

	s.SetMeta(ctx, metaTurnCount, "5")
	s.SetMeta(ctx, metaLastDreaming, "not-a-timestamp")

	// Corrupted timestamp: should not crash, returns false.
	if da.ShouldDream(ctx) {
		t.Error("ShouldDream should be false with corrupted timestamp")
	}
}

func TestDreamingAdapter_ShouldDream_justBelowTimeThreshold(t *testing.T) {
	s := tempStoreForDreaming(t)
	ctx := context.Background()
	da := NewDreamingAdapter(s, nil, nil, "", discardLogger())

	s.SetMeta(ctx, metaTurnCount, "0")
	// 7 hours ago — below the 8-hour threshold.
	recentTime := time.Now().Add(-7 * time.Hour).UTC().Format(time.RFC3339)
	s.SetMeta(ctx, metaLastDreaming, recentTime)

	if da.ShouldDream(ctx) {
		t.Error("ShouldDream should be false when just below time threshold")
	}
}
