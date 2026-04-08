package telegram

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunStateMachineStartEnd(t *testing.T) {
	var mu sync.Mutex
	var patches []StatusPatch

	sink := func(p StatusPatch) {
		mu.Lock()
		patches = append(patches, p)
		mu.Unlock()
	}

	sm := NewRunStateMachine(context.Background(), sink, 0)
	defer sm.Close()

	sm.StartRun()
	sm.StartRun()
	sm.EndRun()
	sm.EndRun()

	mu.Lock()
	defer mu.Unlock()

	if len(patches) != 4 {
		t.Fatalf("got %d, want 4 patches", len(patches))
	}
	if *patches[0].ActiveRuns != 1 || !*patches[0].Busy {
		t.Errorf("patch[0] unexpected: %+v", patches[0])
	}
	if *patches[3].ActiveRuns != 0 || *patches[3].Busy {
		t.Errorf("patch[3] unexpected: %+v", patches[3])
	}
}

func TestRunStateMachineHeartbeat(t *testing.T) {
	var count atomic.Int32

	sm := NewRunStateMachine(context.Background(), func(p StatusPatch) {
		count.Add(1)
	}, 10*time.Millisecond)

	// Wait for at least 2 heartbeats using polling instead of fixed sleep.
	deadline := time.After(2 * time.Second)
	for count.Load() < 2 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for heartbeats, got %d", count.Load())
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	sm.Close()

	// After Close(), the goroutine should have exited (WaitGroup done).
	// Verify no more events arrive.
	countAfter := count.Load()
	time.Sleep(30 * time.Millisecond)
	if count.Load() != countAfter {
		t.Error("heartbeat should stop after Close()")
	}
}

