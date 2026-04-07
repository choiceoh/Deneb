package rlm

import (
	"testing"
	"time"
)

func TestTraceStore_AddAndLatest(t *testing.T) {
	store := NewTraceStore(5)
	if store.Latest() != nil {
		t.Fatal("expected nil from empty store")
	}

	store.Add(Trace{ID: "t1", StartedAt: time.Now(), StopReason: "final"})
	got := store.Latest()
	if got == nil || got.ID != "t1" {
		t.Fatalf("expected t1, got %v", got)
	}

	store.Add(Trace{ID: "t2", StartedAt: time.Now(), StopReason: "max_iterations"})
	got = store.Latest()
	if got.ID != "t2" {
		t.Fatalf("expected t2, got %s", got.ID)
	}
}

func TestTraceStore_Get(t *testing.T) {
	store := NewTraceStore(5)
	store.Add(Trace{ID: "a"})
	store.Add(Trace{ID: "b"})
	store.Add(Trace{ID: "c"})

	if got := store.Get("b"); got == nil || got.ID != "b" {
		t.Fatalf("expected b, got %v", got)
	}
	if got := store.Get("missing"); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestTraceStore_RingBuffer(t *testing.T) {
	store := NewTraceStore(3) // capacity 3
	store.Add(Trace{ID: "1"})
	store.Add(Trace{ID: "2"})
	store.Add(Trace{ID: "3"})
	store.Add(Trace{ID: "4"}) // evicts "1"

	if store.Count() != 3 {
		t.Fatalf("expected count 3, got %d", store.Count())
	}
	if got := store.Get("1"); got != nil {
		t.Fatal("expected 1 to be evicted")
	}
	if got := store.Latest(); got.ID != "4" {
		t.Fatalf("expected latest=4, got %s", got.ID)
	}
}

func TestTraceStore_List(t *testing.T) {
	store := NewTraceStore(10)
	now := time.Now()
	for i := 0; i < 5; i++ {
		store.Add(Trace{
			ID:         "t" + string(rune('a'+i)),
			StartedAt:  now.Add(time.Duration(i) * time.Second),
			Model:      "test-model",
			StopReason: "final",
			UserPrompt: "test prompt",
		})
	}

	list := store.List(3)
	if len(list) != 3 {
		t.Fatalf("expected 3 summaries, got %d", len(list))
	}
	// Newest first.
	if list[0].ID != "te" {
		t.Fatalf("expected te first, got %s", list[0].ID)
	}
}

func TestTraceSummary_Truncation(t *testing.T) {
	tr := Trace{
		ID:         "test",
		StartedAt:  time.Now(),
		UserPrompt: "a very long prompt that exceeds eighty characters and should be truncated to fit nicely in the summary view",
	}
	s := tr.Summary()
	if len(s.Prompt) > 84 { // 80 + "..."
		t.Fatalf("prompt not truncated: %d chars", len(s.Prompt))
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("short", 10); got != "short" {
		t.Fatalf("expected short, got %s", got)
	}
	if got := truncate("this is longer", 4); got != "this..." {
		t.Fatalf("expected this..., got %s", got)
	}
}
