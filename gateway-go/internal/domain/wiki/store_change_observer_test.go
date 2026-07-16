package wiki

import (
	"context"
	"testing"
	"time"
)

// recvChange waits for one observer callback (they arrive on a drain goroutine).
func recvChange(t *testing.T, ch chan string) string {
	t.Helper()
	select {
	case p := <-ch:
		return p
	case <-time.After(2 * time.Second):
		t.Fatal("no change event within 2s")
		return ""
	}
}

func expectQuiet(t *testing.T, ch chan string) {
	t.Helper()
	select {
	case p := <-ch:
		t.Fatalf("unexpected change event %q", p)
	case <-time.After(150 * time.Millisecond):
	}
}

// The native-sync mirror contract: meaningful writes and deletes emit the page's
// relPath; backlink maintenance on OTHER pages (a side effect of the same write)
// stays silent, so a single save can't fan out into N events.
func TestChangeObserver_FiresOnWriteAndDelete_SkipsBacklinkMaintenance(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 16)
	store.SetChangeObserver(ctx, func(relPath string) { got <- relPath })

	// Target page first (so the backlink write below has something to update).
	target := NewPage("타깃", "기타", nil)
	target.Body = "target body"
	if err := store.WritePage("기타/타깃.md", target); err != nil {
		t.Fatal(err)
	}
	if p := recvChange(t, got); p != "기타/타깃.md" {
		t.Fatalf("write event = %q, want 기타/타깃.md", p)
	}

	// A page related to the target: its write emits ONE event for itself; the
	// backlink maintenance write on 타깃 must not emit a second one.
	src := NewPage("소스", "기타", nil)
	src.Body = "source body"
	src.Meta.Related = []string{"기타/타깃.md"}
	if err := store.WritePage("기타/소스.md", src); err != nil {
		t.Fatal(err)
	}
	if p := recvChange(t, got); p != "기타/소스.md" {
		t.Fatalf("write event = %q, want 기타/소스.md", p)
	}
	expectQuiet(t, got)

	// Delete emits the deleted path (backlink cleanup on 타깃 stays silent).
	if err := store.DeletePage("기타/소스.md"); err != nil {
		t.Fatal(err)
	}
	if p := recvChange(t, got); p != "기타/소스.md" {
		t.Fatalf("delete event = %q, want 기타/소스.md", p)
	}
	expectQuiet(t, got)
}

// A move is write-target + delete-source under one lock — both sides emit, so
// clients invalidate the old and new category lists alike.
func TestChangeObserver_MoveEmitsTargetAndSource(t *testing.T) {
	store, err := NewStore(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 16)

	page := NewPage("이동", "기타", nil)
	page.Body = "move body"
	if err := store.WritePage("기타/이동.md", page); err != nil {
		t.Fatal(err)
	}

	store.SetChangeObserver(ctx, func(relPath string) { got <- relPath })
	if err := store.MovePage("기타/이동.md", "프로젝트/이동.md"); err != nil {
		t.Fatal(err)
	}
	first, second := recvChange(t, got), recvChange(t, got)
	if first != "프로젝트/이동.md" || second != "기타/이동.md" {
		t.Fatalf("move events = %q, %q — want target then source", first, second)
	}
}
