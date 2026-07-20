package autonomous

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestComputeInitialDelayAfterRestart covers the restart catch-up math: a recently-run task
// waits out only the remainder of its interval, while a never-run or overdue
// task uses the grace period.
func TestComputeInitialDelayAfterRestart(t *testing.T) {
	grace := 30 * time.Second
	interval := 10 * time.Minute
	now := time.UnixMilli(2_000_000_000_000)

	tests := []struct {
		name      string
		lastRunAt int64
		want      time.Duration
	}{
		{"never run", 0, grace},
		{"negative sentinel", -1, grace},
		{"ran 8m ago leaves 2m", now.Add(-8 * time.Minute).UnixMilli(), 2 * time.Minute},
		{"ran 9m55s ago, 5s remainder below grace", now.Add(-9*time.Minute - 55*time.Second).UnixMilli(), grace},
		{"exactly due", now.Add(-interval).UnixMilli(), grace},
		{"overdue 15m", now.Add(-15 * time.Minute).UnixMilli(), grace},
		{"ran just now waits ~full interval", now.UnixMilli(), interval},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeInitialDelay(tt.lastRunAt, interval, grace, now)
			if got != tt.want {
				t.Errorf("computeInitialDelay(lastRunAt=%d) = %v, want %v", tt.lastRunAt, got, tt.want)
			}
		})
	}
}

// TestStatePersistenceRoundTrip verifies LastRunAt survives a simulated restart:
// one service saves, a fresh service with the same state dir loads it.
func TestStatePersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()
	const taskName = "alpha"
	lastRun := time.Now().Add(-5 * time.Minute).UnixMilli()

	s1 := NewService(nil)
	s1.SetStateDir(dir)
	s1.RegisterTask(&fakeTask{name: taskName, interval: time.Hour})
	s1.mu.Lock()
	s1.taskStatus[taskName].LastRunAt = lastRun
	s1.mu.Unlock()
	s1.saveState()

	s2 := NewService(nil)
	s2.SetStateDir(dir)
	s2.RegisterTask(&fakeTask{name: taskName, interval: time.Hour})
	s2.mu.Lock()
	s2.loadStateLocked()
	got := s2.taskStatus[taskName].LastRunAt
	s2.mu.Unlock()

	if got != lastRun {
		t.Errorf("LastRunAt after reload = %d, want %d", got, lastRun)
	}
}

// TestStatePersistenceWithoutStateDir confirms save/load are safe no-ops when no state dir
// is configured (in-memory-only mode).
func TestStatePersistenceWithoutStateDir(t *testing.T) {
	s := NewService(nil)
	s.RegisterTask(&fakeTask{name: "x", interval: time.Hour})
	s.saveState() // must not panic or write anything
	s.mu.Lock()
	s.loadStateLocked() // must not panic
	s.mu.Unlock()
}

// TestStatePersistenceUnknownTaskIgnored ensures a persisted task that the new
// service never registered is silently skipped (not resurrected into status).
func TestStatePersistenceUnknownTaskIgnored(t *testing.T) {
	dir := t.TempDir()

	s1 := NewService(nil)
	s1.SetStateDir(dir)
	s1.RegisterTask(&fakeTask{name: "gone", interval: time.Hour})
	s1.mu.Lock()
	s1.taskStatus["gone"].LastRunAt = 12345
	s1.mu.Unlock()
	s1.saveState()

	s2 := NewService(nil)
	s2.SetStateDir(dir)
	s2.RegisterTask(&fakeTask{name: "other", interval: time.Hour})
	s2.mu.Lock()
	s2.loadStateLocked()
	_, hasGone := s2.taskStatus["gone"]
	s2.mu.Unlock()

	if hasGone {
		t.Error("unknown persisted task should not be added to taskStatus")
	}
}

// A subset process (live-test registering fewer tasks) must MERGE its LastRunAt
// values and leave unregistered keys on disk — full replace wiped prod-only
// lanes (judge-accuracy) and forced cold-start re-fires after every deploy.
func TestStatePersistenceMergePreservesUnregisteredKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "autonomous_state.json")
	if err := os.WriteFile(path, []byte(`{"judge-accuracy":111,"heartbeat":222}`), 0o600); err != nil {
		t.Fatal(err)
	}

	s := NewService(nil)
	s.SetStateDir(dir)
	s.RegisterTask(&fakeTask{name: "heartbeat", interval: time.Hour})
	s.mu.Lock()
	s.taskStatus["heartbeat"].LastRunAt = 333
	s.mu.Unlock()
	s.saveState()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]int64
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["judge-accuracy"] != 111 {
		t.Fatalf("prod-only key wiped: %+v", got)
	}
	if got["heartbeat"] != 333 {
		t.Fatalf("registered key not updated: %+v", got)
	}
}

// Stop cancels svcCtx; Start must allocate a fresh parent so config-reload
// Stop→Start does not spawn children of a dead context.
func TestStartRecreatesSvcCtxAfterStop(t *testing.T) {
	s := NewService(nil)
	s.RegisterTask(&fakeTask{name: "x", interval: time.Hour})
	s.Start()
	s.mu.Lock()
	first := s.svcCtx
	s.mu.Unlock()
	if first == nil {
		t.Fatal("svcCtx nil after Start")
	}
	s.Stop()
	if err := first.Err(); err == nil {
		t.Fatal("Stop did not cancel the first svcCtx")
	}
	s.Start()
	s.mu.Lock()
	second := s.svcCtx
	s.mu.Unlock()
	if second == nil || second.Err() != nil {
		t.Fatalf("Start did not recreate a live svcCtx: %v", second)
	}
	if second == first {
		t.Fatal("Start reused the cancelled svcCtx")
	}
	// Drain loops so the test process can exit cleanly.
	s.Stop()
}
