package workfeed

import (
	"path/filepath"
	"testing"
	"time"
)

// A snoozed item re-surfaces (as actionable, near the top) once its window
// elapses; one still inside its window stays hidden. This is what restores the
// "나중에" (snooze) vs "완료" (ack) distinction — before, snooze hid an item
// permanently, identical to ack.
func TestSnoozeReSurfacesWhenWindowElapses(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	now := time.Now().UnixMilli()

	// Elapsed snooze (created long ago) — should re-surface near the top.
	if _, err := store.Append(Item{ID: "elapsed", Source: SourceProactive, Title: "Elapsed",
		Status: StatusSnoozed, SnoozedUntilMs: now - 1_000, CreatedAtMs: now - 100_000}); err != nil {
		t.Fatal(err)
	}
	// Snooze still pending — stays hidden.
	if _, err := store.Append(Item{ID: "pending", Source: SourceProactive, Title: "Pending",
		Status: StatusSnoozed, SnoozedUntilMs: now + 1_000_000, CreatedAtMs: now}); err != nil {
		t.Fatal(err)
	}
	// A normal item created between the two timestamps.
	if _, err := store.Append(Item{ID: "normal", Source: SourceProactive, Title: "Normal",
		CreatedAtMs: now - 50_000}); err != nil {
		t.Fatal(err)
	}

	items, total, err := store.List(10, false)
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 || len(items) != 2 {
		t.Fatalf("visible = %d (total %d), want 2 (elapsed + normal; pending hidden): %+v", len(items), total, items)
	}
	// Re-surfaced snooze sorts above the older normal item (wake time is recent).
	if items[0].ID != "elapsed" || items[1].ID != "normal" {
		t.Fatalf("order = %q, %q; want elapsed (re-surfaced, prominent) then normal", items[0].ID, items[1].ID)
	}
	if items[0].Status != StatusUnread {
		t.Errorf("re-surfaced status = %q, want %q (actionable again)", items[0].Status, StatusUnread)
	}
	for _, it := range items {
		if it.ID == "pending" {
			t.Errorf("pending snooze is still within its window and must stay hidden")
		}
	}
}

// RunAction snooze sets a future re-surface window, removes the item from the
// current feed, and leaves the snooze action available (non-terminal) so it can
// be snoozed again after it returns.
func TestSnoozeActionSetsReSurfaceWindow(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))
	item, err := store.Append(Item{ID: "x", Source: SourceProactive, Title: "X", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}

	before := time.Now().UnixMilli()
	res, err := store.RunAction(item.ID, ActionSnooze)
	if err != nil {
		t.Fatal(err)
	}
	if !res.RemoveFromFeed || res.Item.Status != StatusSnoozed {
		t.Fatalf("res = %+v, want snoozed + removed", res)
	}
	if res.Item.SnoozedUntilMs <= before {
		t.Errorf("SnoozedUntilMs = %d, want a future wake time (> %d)", res.Item.SnoozedUntilMs, before)
	}
	for _, a := range res.Item.Actions {
		if a.Kind == ActionSnooze && a.Status == "done" {
			t.Errorf("snooze action marked done; it must stay available for re-snooze")
		}
	}

	// Hidden immediately after snooze (window not elapsed yet).
	items, _, _ := store.List(10, false)
	for _, it := range items {
		if it.ID == "x" {
			t.Errorf("snoozed item still visible immediately after snooze")
		}
	}
}
