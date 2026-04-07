package agent

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestJobTracker_LifecycleStartEnd(t *testing.T) {
	jt := NewJobTracker(nil)

	now := time.Now().UnixMilli()

	// Start.
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-1", Phase: "start", Ts: now})
	if !jt.IsRunning("run-1") {
		t.Error("expected run-1 to be running after start")
	}

	// End.
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-1", Phase: "end", Ts: now + 1000})
	if jt.IsRunning("run-1") {
		t.Error("expected run-1 to not be running after end")
	}

	snap := jt.CachedSnapshot("run-1")
	if snap == nil {
		t.Fatal("expected cached snapshot")
	}
	if snap.Status != RunStatusOK {
		t.Errorf("expected status OK, got %s", snap.Status)
	}
}

func TestJobTracker_AbortedRunTimeout(t *testing.T) {
	jt := NewJobTracker(nil)
	now := time.Now().UnixMilli()

	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-2", Phase: "start", Ts: now})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-2", Phase: "end", Ts: now + 500, Aborted: true})

	snap := jt.CachedSnapshot("run-2")
	if snap == nil {
		t.Fatal("expected cached snapshot")
	}
	if snap.Status != RunStatusTimeout {
		t.Errorf("expected status timeout, got %s", snap.Status)
	}
}

func TestJobTracker_ErrorGraceWindow(t *testing.T) {
	jt := NewJobTracker(nil)
	now := time.Now().UnixMilli()

	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-3", Phase: "start", Ts: now})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-3", Phase: "error", Ts: now + 100, Error: "auth failed"})

	// Immediately after error, should NOT be cached (grace window active).
	snap := jt.CachedSnapshot("run-3")
	if snap != nil {
		t.Error("expected no cached snapshot during grace window")
	}

	// Restart clears pending error.
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-3", Phase: "start", Ts: now + 200})
	jt.mu.Lock()
	_, hasPending := jt.pending["run-3"]
	jt.mu.Unlock()
	if hasPending {
		t.Error("expected pending error to be cleared after restart")
	}
}

func TestJobTracker_WaitForJob_CachedResult(t *testing.T) {
	jt := NewJobTracker(nil)
	now := time.Now().UnixMilli()

	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-4", Phase: "start", Ts: now})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-4", Phase: "end", Ts: now + 500})

	snap := jt.WaitForJob(context.Background(), "run-4", 1000, false)
	if snap == nil {
		t.Fatal("expected cached snapshot")
	}
	if snap.Status != RunStatusOK {
		t.Errorf("expected OK, got %s", snap.Status)
	}
}

func TestJobTracker_WaitForJob_Timeout(t *testing.T) {
	jt := NewJobTracker(nil)

	snap := jt.WaitForJob(context.Background(), "nonexistent", 100, false)
	if snap != nil {
		t.Error("expected nil for timeout")
	}
}

func TestJobTracker_WaitForJob_ZeroTimeout(t *testing.T) {
	jt := NewJobTracker(nil)

	snap := jt.WaitForJob(context.Background(), "run-x", 0, false)
	if snap != nil {
		t.Error("expected nil for zero timeout")
	}
}

func TestJobTracker_ActiveRunCount(t *testing.T) {
	jt := NewJobTracker(nil)
	now := time.Now().UnixMilli()

	if jt.ActiveRunCount() != 0 {
		t.Error("expected 0 active runs")
	}

	jt.OnLifecycleEvent(LifecycleEvent{RunID: "a", Phase: "start", Ts: now})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "b", Phase: "start", Ts: now})

	if jt.ActiveRunCount() != 2 {
		t.Errorf("expected 2 active runs, got %d", jt.ActiveRunCount())
	}

	jt.OnLifecycleEvent(LifecycleEvent{RunID: "a", Phase: "end", Ts: now + 100})
	if jt.ActiveRunCount() != 1 {
		t.Errorf("expected 1 active run, got %d", jt.ActiveRunCount())
	}
}

func TestJobTracker_WaitForJob_ErrorThenRestartThenEnd(t *testing.T) {
	jt := NewJobTracker(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resultCh := make(chan *RunSnapshot, 1)
	go func() {
		resultCh <- jt.WaitForJob(ctx, "run-retry", 2500, true)
	}()

	now := time.Now().UnixMilli()
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-retry", Phase: "start", Ts: now})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-retry", Phase: "error", Ts: now + 10, Error: "temporary"})

	// Simulate autonomous retry/restart within grace window.
	time.Sleep(20 * time.Millisecond)
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-retry", Phase: "start", Ts: now + 30})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-retry", Phase: "end", Ts: now + 60})

	select {
	case snap := <-resultCh:
		if snap == nil {
			t.Fatal("expected snapshot after restart/end sequence")
		}
		if snap.Status != RunStatusOK {
			t.Fatalf("expected status OK, got %s", snap.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("WaitForJob did not return in time")
	}
}

func TestJobTracker_WaitForJob_IgnoresStaleCacheWhenRequested(t *testing.T) {
	jt := NewJobTracker(nil)
	now := time.Now().UnixMilli()

	// Seed terminal cache from a previous run.
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-stale", Phase: "start", Ts: now})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-stale", Phase: "end", Ts: now + 50})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var returned atomic.Bool
	resultCh := make(chan *RunSnapshot, 1)
	go func() {
		snap := jt.WaitForJob(ctx, "run-stale", 1500, true)
		if snap != nil {
			returned.Store(true)
		}
		resultCh <- snap
	}()

	// Ensure we're truly waiting (cached result should be ignored).
	time.Sleep(30 * time.Millisecond)
	if returned.Load() {
		t.Fatal("expected WaitForJob(ignoreCached=true) to wait for new lifecycle events")
	}

	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-stale", Phase: "start", Ts: now + 100})
	jt.OnLifecycleEvent(LifecycleEvent{RunID: "run-stale", Phase: "end", Ts: now + 200})

	snap := <-resultCh
	if snap == nil {
		t.Fatal("expected fresh snapshot after new lifecycle events")
	}
	if snap.Status != RunStatusOK {
		t.Fatalf("expected OK status, got %s", snap.Status)
	}
	if snap.EndedAt == nil || *snap.EndedAt != now+200 {
		t.Fatalf("expected endedAt=%d, got %v", now+200, snap.EndedAt)
	}
}
