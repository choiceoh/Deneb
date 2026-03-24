package agent

import (
	"context"
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

	snap := jt.GetCachedSnapshot("run-1")
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

	snap := jt.GetCachedSnapshot("run-2")
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
	snap := jt.GetCachedSnapshot("run-3")
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
