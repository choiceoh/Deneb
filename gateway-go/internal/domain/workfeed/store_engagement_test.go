package workfeed

import (
	"path/filepath"
	"testing"
)

func TestEngagement(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))

	const now int64 = 2_000_000_000_000 // realistic ms epoch (~2033); must stay positive after subtracting the window
	const stale int64 = 48 * 60 * 60 * 1000
	old := now - stale - 1 // older than the window → ignored if unread
	fresh := now - 1000    // within the window → pending if unread

	seed := func(id, source, status string, created int64) {
		t.Helper()
		if _, err := store.Append(Item{ID: id, Source: source, Title: id, Status: status, CreatedAtMs: created}); err != nil {
			t.Fatalf("append %s: %v", id, err)
		}
	}
	// Engaged: the user acted (ack) or deferred (snooze) — both count as engaged.
	seed("a", SourceProactive, StatusAcked, old)
	seed("b", SourceProactive, StatusSnoozed, old)
	// Ignored: unread past the window, across two sources.
	seed("c", SourceProactive, StatusUnread, old)
	seed("d", "dream:foo", StatusUnread, old)
	seed("e", SourceProactive, StatusUnread, old)
	// Pending: unread but still fresh — too new to judge.
	seed("f", SourceProactive, StatusUnread, fresh)

	stat, err := store.Engagement(now, stale)
	if err != nil {
		t.Fatalf("Engagement: %v", err)
	}
	if stat.Total != 6 || stat.Engaged != 2 || stat.Ignored != 3 || stat.Pending != 1 {
		t.Fatalf("counts wrong: %+v", stat)
	}
	if stat.BySource[SourceProactive] != 2 || stat.BySource["dream:foo"] != 1 {
		t.Fatalf("ignored-by-source wrong: %v", stat.BySource)
	}
	// FTR = ignored / (engaged + ignored) = 3 / 5 = 0.6.
	if got := stat.FTR(); got != 0.6 {
		t.Fatalf("FTR=%.2f, want 0.60", got)
	}
}

func TestEngagement_EmptyAndAllPending(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "workfeed.jsonl"))

	empty, err := store.Engagement(1000, 1000)
	if err != nil {
		t.Fatalf("Engagement empty: %v", err)
	}
	if empty.Total != 0 || empty.FTR() != 0 {
		t.Fatalf("empty store must yield zero stat, got %+v (FTR %.2f)", empty, empty.FTR())
	}

	// A staleWindow of 0 means nothing is ever judged stale — all unread are pending.
	if _, err := store.Append(Item{ID: "x", Source: SourceProactive, Title: "x", Status: StatusUnread, CreatedAtMs: 1}); err != nil {
		t.Fatal(err)
	}
	all, _ := store.Engagement(1_000_000, 0)
	if all.Pending != 1 || all.Ignored != 0 || all.FTR() != 0 {
		t.Fatalf("staleWindow=0 must leave cards pending, got %+v", all)
	}
}
