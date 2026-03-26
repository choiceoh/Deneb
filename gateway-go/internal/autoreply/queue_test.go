package autoreply

import (
	"testing"
)

func TestFollowupQueueRegistry_GetOrCreate(t *testing.T) {
	r := NewFollowupQueueRegistry()
	settings := FollowupQueueSettings{
		Mode:       FollowupModeCollect,
		DebounceMs: 500,
		Cap:        10,
		DropPolicy: FollowupDropOld,
	}

	q := r.GetOrCreate("session:main", settings)
	if q == nil {
		t.Fatal("expected non-nil queue")
	}
	if q.Mode != FollowupModeCollect {
		t.Errorf("expected mode=collect, got %s", q.Mode)
	}
	if q.DebounceMs != 500 {
		t.Errorf("expected debounce=500, got %d", q.DebounceMs)
	}
	if q.Cap != 10 {
		t.Errorf("expected cap=10, got %d", q.Cap)
	}

	// Second call returns same object.
	q2 := r.GetOrCreate("session:main", settings)
	if q != q2 {
		t.Error("expected same queue object on second call")
	}

	// Depth.
	if r.Depth("session:main") != 0 {
		t.Errorf("expected depth=0")
	}
}

func TestFollowupQueueRegistry_Clear(t *testing.T) {
	r := NewFollowupQueueRegistry()
	q := r.GetOrCreate("k", FollowupQueueSettings{Mode: FollowupModeSteer})
	q.Items = append(q.Items, FollowupRun{Prompt: "hello"})
	q.DroppedCount = 2

	cleared := r.Clear("k")
	if cleared != 3 {
		t.Errorf("expected cleared=3, got %d", cleared)
	}
	if r.GetExisting("k") != nil {
		t.Error("expected queue to be deleted after clear")
	}
}

func TestEnqueueFollowupRun_basic(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := newRecentMessageIDCache()
	settings := FollowupQueueSettings{Mode: FollowupModeSteer, Cap: 5}

	ok := r.EnqueueFollowupRun("k", FollowupRun{Prompt: "hello", MessageID: "m1"}, settings, DedupeMessageID, cache)
	if !ok {
		t.Error("expected enqueue to succeed")
	}
	if r.Depth("k") != 1 {
		t.Errorf("expected depth=1, got %d", r.Depth("k"))
	}

	// Duplicate should be rejected.
	ok = r.EnqueueFollowupRun("k", FollowupRun{Prompt: "hello", MessageID: "m1"}, settings, DedupeMessageID, cache)
	if ok {
		t.Error("expected duplicate to be rejected")
	}
	if r.Depth("k") != 1 {
		t.Errorf("expected depth=1 after dup, got %d", r.Depth("k"))
	}
}

func TestEnqueueFollowupRun_dropPolicy(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := newRecentMessageIDCache()
	settings := FollowupQueueSettings{Mode: FollowupModeSteer, Cap: 2, DropPolicy: FollowupDropNew}

	r.EnqueueFollowupRun("k", FollowupRun{Prompt: "1"}, settings, DedupeNone, cache)
	r.EnqueueFollowupRun("k", FollowupRun{Prompt: "2"}, settings, DedupeNone, cache)

	// At capacity, new item should be dropped.
	ok := r.EnqueueFollowupRun("k", FollowupRun{Prompt: "3"}, settings, DedupeNone, cache)
	if ok {
		t.Error("expected drop-new to reject")
	}
	if r.Depth("k") != 2 {
		t.Errorf("expected depth=2, got %d", r.Depth("k"))
	}
}

func TestEnqueueFollowupRun_dropOld(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := newRecentMessageIDCache()
	settings := FollowupQueueSettings{Mode: FollowupModeSteer, Cap: 2, DropPolicy: FollowupDropOld}

	r.EnqueueFollowupRun("k", FollowupRun{Prompt: "1"}, settings, DedupeNone, cache)
	r.EnqueueFollowupRun("k", FollowupRun{Prompt: "2"}, settings, DedupeNone, cache)
	ok := r.EnqueueFollowupRun("k", FollowupRun{Prompt: "3"}, settings, DedupeNone, cache)
	if !ok {
		t.Error("expected drop-old to accept new item")
	}
	q := r.GetExisting("k")
	if q == nil || len(q.Items) != 2 {
		t.Fatalf("expected 2 items, got %v", q)
	}
	// Oldest (prompt "1") should have been dropped; items should be "2" and "3".
	if q.Items[0].Prompt != "2" || q.Items[1].Prompt != "3" {
		t.Errorf("expected items [2,3], got [%s,%s]", q.Items[0].Prompt, q.Items[1].Prompt)
	}
}

func TestNormalizeFollowupQueueMode(t *testing.T) {
	tests := []struct {
		input string
		want  FollowupQueueMode
	}{
		{"steer", FollowupModeSteer},
		{"Steer", FollowupModeSteer},
		{"collect", FollowupModeCollect},
		{"followup", FollowupModeFollowup},
		{"interrupt", FollowupModeInterrupt},
		{"steer-backlog", FollowupModeSteerBacklog},
		{"queue", FollowupModeSteer},
		{"", ""},
		{"unknown", ""},
	}
	for _, tt := range tests {
		got := NormalizeFollowupQueueMode(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeFollowupQueueMode(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeFollowupDropPolicy(t *testing.T) {
	tests := []struct {
		input string
		want  FollowupDropPolicy
	}{
		{"old", FollowupDropOld},
		{"oldest", FollowupDropOld},
		{"new", FollowupDropNew},
		{"summarize", FollowupDropSummarize},
		{"", ""},
		{"bad", ""},
	}
	for _, tt := range tests {
		got := NormalizeFollowupDropPolicy(tt.input)
		if got != tt.want {
			t.Errorf("NormalizeFollowupDropPolicy(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestExtractQueueDirective(t *testing.T) {
	tests := []struct {
		input   string
		hasDir  bool
		mode    FollowupQueueMode
		reset   bool
		cleaned string
	}{
		{"hello /queue collect world", true, FollowupModeCollect, false, "hello world"},
		{"/queue reset", true, "", true, ""},
		{"/queue steer", true, FollowupModeSteer, false, ""},
		{"no queue here", false, "", false, "no queue here"},
		{"", false, "", false, ""},
	}
	for _, tt := range tests {
		got := ExtractQueueDirective(tt.input)
		if got.HasDirective != tt.hasDir {
			t.Errorf("ExtractQueueDirective(%q): HasDirective=%v, want %v", tt.input, got.HasDirective, tt.hasDir)
		}
		if got.QueueMode != tt.mode {
			t.Errorf("ExtractQueueDirective(%q): QueueMode=%q, want %q", tt.input, got.QueueMode, tt.mode)
		}
		if got.QueueReset != tt.reset {
			t.Errorf("ExtractQueueDirective(%q): QueueReset=%v, want %v", tt.input, got.QueueReset, tt.reset)
		}
		if got.Cleaned != tt.cleaned {
			t.Errorf("ExtractQueueDirective(%q): Cleaned=%q, want %q", tt.input, got.Cleaned, tt.cleaned)
		}
	}
}

func TestClearSessionQueues(t *testing.T) {
	r := NewFollowupQueueRegistry()
	r.GetOrCreate("k1", FollowupQueueSettings{Mode: FollowupModeSteer})
	q := r.GetExisting("k1")
	q.Items = append(q.Items, FollowupRun{Prompt: "a"}, FollowupRun{Prompt: "b"})

	result := ClearSessionQueues(r, nil, []string{"k1", "k1", "k2"})
	if result.FollowupCleared != 2 {
		t.Errorf("expected followupCleared=2, got %d", result.FollowupCleared)
	}
	if len(result.Keys) != 2 {
		t.Errorf("expected 2 unique keys, got %d", len(result.Keys))
	}
}

func TestResolveFollowupQueueSettings(t *testing.T) {
	s := ResolveFollowupQueueSettings(ResolveFollowupQueueSettingsParams{
		Channel: "telegram",
	})
	if s.Mode != FollowupModeCollect {
		t.Errorf("expected default mode=collect, got %s", s.Mode)
	}
	if s.DebounceMs != DefaultFollowupDebounceMs {
		t.Errorf("expected default debounce=%d, got %d", DefaultFollowupDebounceMs, s.DebounceMs)
	}

	s2 := ResolveFollowupQueueSettings(ResolveFollowupQueueSettingsParams{
		InlineMode: FollowupModeSteer,
		DebounceMs: 2000,
		Cap:        50,
	})
	if s2.Mode != FollowupModeSteer {
		t.Errorf("expected inline mode=steer, got %s", s2.Mode)
	}
	if s2.DebounceMs != 2000 {
		t.Errorf("expected debounce=2000, got %d", s2.DebounceMs)
	}
}
