package queue

import (
	"github.com/choiceoh/deneb/gateway-go/internal/autoreply/types"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestFollowupQueueRegistry_GetOrCreate(t *testing.T) {
	r := NewFollowupQueueRegistry()
	settings := types.FollowupQueueSettings{
		Mode:       types.FollowupModeCollect,
		DebounceMs: 500,
		Cap:        10,
		DropPolicy: types.FollowupDropOld,
	}

	q := r.GetOrCreate("session:main", settings)
	if q == nil {
		t.Fatal("expected non-nil queue")
	}
	if q.Mode != types.FollowupModeCollect {
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
	q := r.GetOrCreate("k", types.FollowupQueueSettings{Mode: types.FollowupModeSteer})
	q.Items = append(q.Items, types.FollowupRun{Prompt: "hello"})
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
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeSteer, Cap: 5}

	ok := r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "hello", MessageID: "m1"}, settings, types.DedupeMessageID, cache)
	if !ok {
		t.Error("expected enqueue to succeed")
	}
	if r.Depth("k") != 1 {
		t.Errorf("expected depth=1, got %d", r.Depth("k"))
	}

	// Duplicate should be rejected.
	ok = r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "hello", MessageID: "m1"}, settings, types.DedupeMessageID, cache)
	if ok {
		t.Error("expected duplicate to be rejected")
	}
	if r.Depth("k") != 1 {
		t.Errorf("expected depth=1 after dup, got %d", r.Depth("k"))
	}
}

func TestEnqueueFollowupRun_dropPolicy(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeSteer, Cap: 2, DropPolicy: types.FollowupDropNew}

	r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "1"}, settings, types.DedupeNone, cache)
	r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "2"}, settings, types.DedupeNone, cache)

	// At capacity, new item should be dropped.
	ok := r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "3"}, settings, types.DedupeNone, cache)
	if ok {
		t.Error("expected drop-new to reject")
	}
	if r.Depth("k") != 2 {
		t.Errorf("expected depth=2, got %d", r.Depth("k"))
	}
}

func TestEnqueueFollowupRun_dropOld(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeSteer, Cap: 2, DropPolicy: types.FollowupDropOld}

	r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "1"}, settings, types.DedupeNone, cache)
	r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "2"}, settings, types.DedupeNone, cache)
	ok := r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "3"}, settings, types.DedupeNone, cache)
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
		want  types.FollowupQueueMode
	}{
		{"steer", types.FollowupModeSteer},
		{"Steer", types.FollowupModeSteer},
		{"collect", types.FollowupModeCollect},
		{"followup", types.FollowupModeFollowup},
		{"interrupt", types.FollowupModeInterrupt},
		{"steer-backlog", types.FollowupModeSteerBacklog},
		{"queue", types.FollowupModeSteer},
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
		want  types.FollowupDropPolicy
	}{
		{"old", types.FollowupDropOld},
		{"oldest", types.FollowupDropOld},
		{"new", types.FollowupDropNew},
		{"summarize", types.FollowupDropSummarize},
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
		mode    types.FollowupQueueMode
		reset   bool
		cleaned string
	}{
		{"hello /queue collect world", true, types.FollowupModeCollect, false, "hello world"},
		{"/queue reset", true, "", true, ""},
		{"/queue steer", true, types.FollowupModeSteer, false, ""},
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
	r.GetOrCreate("k1", types.FollowupQueueSettings{Mode: types.FollowupModeSteer})
	q := r.GetExisting("k1")
	q.Items = append(q.Items, types.FollowupRun{Prompt: "a"}, types.FollowupRun{Prompt: "b"})

	result := ClearSessionQueues(r, nil, []string{"k1", "k1", "k2"})
	if result.FollowupCleared != 2 {
		t.Errorf("expected followupCleared=2, got %d", result.FollowupCleared)
	}
	if len(result.Keys) != 2 {
		t.Errorf("expected 2 unique keys, got %d", len(result.Keys))
	}
}

func TestFollowupQueue_ConcurrentEnqueue(t *testing.T) {
	// Verify concurrent enqueue does not race or corrupt the queue.
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeSteer, Cap: 200}

	const numEnqueues = 100
	var wg sync.WaitGroup
	for i := 0; i < numEnqueues; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			r.EnqueueFollowupRun("k", types.FollowupRun{
				Prompt:    fmt.Sprintf("msg-%d", idx),
				MessageID: fmt.Sprintf("id-%d", idx),
			}, settings, types.DedupeMessageID, cache)
		}(i)
	}
	wg.Wait()

	depth := r.Depth("k")
	if depth != numEnqueues {
		t.Errorf("expected depth=%d, got %d", numEnqueues, depth)
	}
}

func TestFollowupDrainService_basic(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeSteer, Cap: 100, DebounceMs: 1}

	// Pre-enqueue items.
	for i := 0; i < 5; i++ {
		r.EnqueueFollowupRun("k", types.FollowupRun{
			Prompt:    fmt.Sprintf("msg-%d", i),
			MessageID: fmt.Sprintf("id-%d", i),
		}, settings, types.DedupeMessageID, cache)
	}

	var drainedCount int
	var drainedMu sync.Mutex
	drainService := NewFollowupDrainService(r, func(msg string) { t.Log(msg) })
	drainService.ScheduleDrain("k", func(run types.FollowupRun) error {
		drainedMu.Lock()
		drainedCount++
		drainedMu.Unlock()
		return nil
	})

	// Wait for drain.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		drainedMu.Lock()
		n := drainedCount
		drainedMu.Unlock()
		if n >= 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	drainedMu.Lock()
	count := drainedCount
	drainedMu.Unlock()
	if count != 5 {
		t.Errorf("expected 5 drained, got %d", count)
	}
}

func TestResolveFollowupQueueSettings(t *testing.T) {
	s := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		Channel: "telegram",
	})
	if s.Mode != types.FollowupModeCollect {
		t.Errorf("expected default mode=collect, got %s", s.Mode)
	}
	if s.DebounceMs != DefaultFollowupDebounceMs {
		t.Errorf("expected default debounce=%d, got %d", DefaultFollowupDebounceMs, s.DebounceMs)
	}

	s2 := ResolveFollowupQueueSettings(types.ResolveFollowupQueueSettingsParams{
		InlineMode: types.FollowupModeSteer,
		DebounceMs: 2000,
		Cap:        50,
	})
	if s2.Mode != types.FollowupModeSteer {
		t.Errorf("expected inline mode=steer, got %s", s2.Mode)
	}
	if s2.DebounceMs != 2000 {
		t.Errorf("expected debounce=2000, got %d", s2.DebounceMs)
	}
}
