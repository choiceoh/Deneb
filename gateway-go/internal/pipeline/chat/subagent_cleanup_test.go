package chat

import (
	"sync"
	"testing"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/runtime/session"
)

// waitForChildStatus polls (bus delivery is async per subscriber) until the
// child reaches the wanted status or the deadline passes.
func waitForChildStatus(t *testing.T, sm *session.Manager, key string, want session.RunStatus) *session.Session {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if c := sm.Get(key); c != nil && c.Status == want {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
	c := sm.Get(key)
	if c == nil {
		t.Fatalf("child %q disappeared while waiting for status %s", key, want)
	}
	t.Fatalf("child %q status = %s, want %s", key, c.Status, want)
	return nil
}

func startRunning(t *testing.T, sm *session.Manager, key string, spawnedBy string) {
	t.Helper()
	s := sm.Create(key, session.KindDirect)
	s.SpawnedBy = spawnedBy
	s.Status = session.StatusRunning
	now := time.Now().UnixMilli()
	s.StartedAt = &now
	if err := sm.Set(s); err != nil {
		t.Fatalf("set %q running: %v", key, err)
	}
}

// Killing a parent must interrupt and kill its running children, marking them
// with the cascade reason so the notifier treats them as bookkeeping.
func TestSubagentCleanup_ParentKilledKillsRunningChildren(t *testing.T) {
	sm := session.NewManager()
	startRunning(t, sm, "client:main", "")
	startRunning(t, sm, "client:main:worker:1", "client:main")

	var mu sync.Mutex
	var interrupted []string
	unsub := StartSubagentCleanup(SubagentCleanupDeps{
		Sessions: func() *session.Manager { return sm },
		InterruptRun: func(k string) {
			mu.Lock()
			interrupted = append(interrupted, k)
			mu.Unlock()
		},
	})
	defer unsub()

	parent := sm.Get("client:main")
	parent.Status = session.StatusKilled
	if err := sm.Set(parent); err != nil {
		t.Fatalf("kill parent: %v", err)
	}

	child := waitForChildStatus(t, sm, "client:main:worker:1", session.StatusKilled)
	if child.FailureReason != subagentParentTerminatedReason {
		t.Errorf("child failure reason = %q, want %q", child.FailureReason, subagentParentTerminatedReason)
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(interrupted)
		mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child run was never interrupted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if interrupted[0] != "client:main:worker:1" {
		t.Errorf("interrupted %q, want child key", interrupted[0])
	}
}

// Deleting a parent session must also cascade to its running children.
func TestSubagentCleanup_ParentDeletedKillsRunningChildren(t *testing.T) {
	sm := session.NewManager()
	startRunning(t, sm, "client:main", "")
	startRunning(t, sm, "client:main:worker:1", "client:main")

	unsub := StartSubagentCleanup(SubagentCleanupDeps{
		Sessions:     func() *session.Manager { return sm },
		InterruptRun: func(string) {},
	})
	defer unsub()

	if !sm.Delete("client:main") {
		t.Fatal("delete parent failed")
	}

	waitForChildStatus(t, sm, "client:main:worker:1", session.StatusKilled)
}

// A parent finishing normally (Done) must NOT touch its children — async
// children outliving individual parent turns is the designed behavior.
func TestSubagentCleanup_ParentDoneLeavesChildrenAlone(t *testing.T) {
	sm := session.NewManager()
	startRunning(t, sm, "client:main", "")
	startRunning(t, sm, "client:main:worker:1", "client:main")

	unsub := StartSubagentCleanup(SubagentCleanupDeps{
		Sessions:     func() *session.Manager { return sm },
		InterruptRun: func(string) {},
	})
	defer unsub()

	parent := sm.Get("client:main")
	parent.Status = session.StatusDone
	if err := sm.Set(parent); err != nil {
		t.Fatalf("finish parent: %v", err)
	}

	// Give the bus time to (not) act, then assert the child is untouched.
	time.Sleep(100 * time.Millisecond)
	if c := sm.Get("client:main:worker:1"); c == nil || c.Status != session.StatusRunning {
		t.Fatalf("child should still be running, got %+v", c)
	}
}

// Terminal children are left alone on cascade — their results stay inspectable.
func TestSubagentCleanup_TerminalChildrenUntouched(t *testing.T) {
	sm := session.NewManager()
	startRunning(t, sm, "client:main", "")
	startRunning(t, sm, "client:main:worker:1", "client:main")

	// Child finished before the parent was killed.
	child := sm.Get("client:main:worker:1")
	child.Status = session.StatusDone
	child.LastOutput = "result"
	if err := sm.Set(child); err != nil {
		t.Fatalf("finish child: %v", err)
	}

	unsub := StartSubagentCleanup(SubagentCleanupDeps{
		Sessions:     func() *session.Manager { return sm },
		InterruptRun: func(string) {},
	})
	defer unsub()

	parent := sm.Get("client:main")
	parent.Status = session.StatusKilled
	if err := sm.Set(parent); err != nil {
		t.Fatalf("kill parent: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	c := sm.Get("client:main:worker:1")
	if c == nil || c.Status != session.StatusDone || c.LastOutput != "result" {
		t.Fatalf("done child should be untouched, got %+v", c)
	}
}
