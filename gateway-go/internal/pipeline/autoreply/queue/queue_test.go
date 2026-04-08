package queue

import (
	"fmt"
	"github.com/choiceoh/deneb/gateway-go/internal/pipeline/autoreply/types"
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
		DropPolicy: types.FollowupDropSummarize,
	}

	q := r.GetOrCreate("session:main", settings)
	if q == nil {
		t.Fatal("expected non-nil queue")
	}
	if q.Mode != types.FollowupModeCollect {
		t.Errorf("got %s, want mode=collect", q.Mode)
	}
	if q.DebounceMs != 500 {
		t.Errorf("got %d, want debounce=500", q.DebounceMs)
	}
	if q.Cap != 10 {
		t.Errorf("got %d, want cap=10", q.Cap)
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


func TestEnqueueFollowupRun_basic(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeCollect, Cap: 5}

	ok := r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "hello", MessageID: "m1"}, settings, types.DedupeMessageID, cache)
	if !ok {
		t.Error("expected enqueue to succeed")
	}
	if r.Depth("k") != 1 {
		t.Errorf("got %d, want depth=1", r.Depth("k"))
	}

	// Duplicate should be rejected.
	ok = r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "hello", MessageID: "m1"}, settings, types.DedupeMessageID, cache)
	if ok {
		t.Error("expected duplicate to be rejected")
	}
	if r.Depth("k") != 1 {
		t.Errorf("got %d, want depth=1 after dup", r.Depth("k"))
	}
}

func TestEnqueueFollowupRun_summarizeDropPolicy(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	// Drop policy is always summarize.
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeCollect, Cap: 2, DropPolicy: types.FollowupDropSummarize}

	r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "1"}, settings, types.DedupeNone, cache)
	r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "2"}, settings, types.DedupeNone, cache)

	// At capacity, new item should be summarized and dropped.
	ok := r.EnqueueFollowupRun("k", types.FollowupRun{Prompt: "3"}, settings, types.DedupeNone, cache)
	if ok {
		t.Error("expected summarize-drop to reject")
	}
	if r.Depth("k") != 2 {
		t.Errorf("got %d, want depth=2", r.Depth("k"))
	}
	// Check that the dropped item was summarized.
	q := r.Existing("k")
	if q == nil {
		t.Fatal("expected queue to exist")
	}
	if q.DroppedCount != 1 {
		t.Errorf("got %d, want droppedCount=1", q.DroppedCount)
	}
	if len(q.SummaryLines) != 1 || q.SummaryLines[0] != "3" {
		t.Errorf("got %v, want summary line '3'", q.SummaryLines)
	}
}

func TestClearSessionQueues(t *testing.T) {
	r := NewFollowupQueueRegistry()
	r.GetOrCreate("k1", types.FollowupQueueSettings{Mode: types.FollowupModeCollect})
	q := r.Existing("k1")
	q.Items = append(q.Items, types.FollowupRun{Prompt: "a"}, types.FollowupRun{Prompt: "b"})

	result := ClearSessionQueues(r, nil, []string{"k1", "k1", "k2"})
	if result.FollowupCleared != 2 {
		t.Errorf("got %d, want followupCleared=2", result.FollowupCleared)
	}
	if len(result.Keys) != 2 {
		t.Errorf("got %d, want 2 unique keys", len(result.Keys))
	}
}

func TestFollowupQueue_ConcurrentEnqueue(t *testing.T) {
	// Verify concurrent enqueue does not race or corrupt the queue.
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeCollect, Cap: 200}

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
		t.Errorf("got %d, want depth=%d", depth, numEnqueues)
	}
}

func TestFollowupDrainService_basic(t *testing.T) {
	r := NewFollowupQueueRegistry()
	cache := NewRecentMessageIDCache()
	settings := types.FollowupQueueSettings{Mode: types.FollowupModeCollect, Cap: 100, DebounceMs: 1}

	// Pre-enqueue items.
	for i := 0; i < 5; i++ {
		r.EnqueueFollowupRun("k", types.FollowupRun{
			Prompt:             fmt.Sprintf("msg-%d", i),
			MessageID:          fmt.Sprintf("id-%d", i),
			OriginatingChannel: "telegram",
			OriginatingTo:      "bot",
			Run:                &types.FollowupRunContext{AgentID: "test"},
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

	// In collect mode, all 5 items are batched into a single callback.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		drainedMu.Lock()
		n := drainedCount
		drainedMu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	drainedMu.Lock()
	count := drainedCount
	drainedMu.Unlock()
	if count < 1 {
		t.Errorf("got %d, want at least 1 drain callback (collect batches items)", count)
	}
}
