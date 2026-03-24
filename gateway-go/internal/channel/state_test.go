package channel

import (
	"context"
	"sync"
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
		t.Fatalf("expected 4 patches, got %d", len(patches))
	}
	if *patches[0].ActiveRuns != 1 || !*patches[0].Busy {
		t.Errorf("patch[0] unexpected: %+v", patches[0])
	}
	if *patches[3].ActiveRuns != 0 || *patches[3].Busy {
		t.Errorf("patch[3] unexpected: %+v", patches[3])
	}
}

func TestRunStateMachineHeartbeat(t *testing.T) {
	var mu sync.Mutex
	var count int

	sm := NewRunStateMachine(context.Background(), func(p StatusPatch) {
		mu.Lock()
		count++
		mu.Unlock()
	}, 10*time.Millisecond)
	time.Sleep(35 * time.Millisecond)
	sm.Close()

	mu.Lock()
	defer mu.Unlock()
	if count < 2 {
		t.Errorf("expected at least 2 heartbeat patches, got %d", count)
	}
}
