package autonomous

import (
	"testing"
	"time"
)

// TestComputeInitialDelay covers the restart catch-up math: a recently-run task
// waits out only the remainder of its interval, while a never-run or overdue
// task uses the grace period.
func TestComputeInitialDelay(t *testing.T) {
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

// TestStatePersistenceNoDir confirms save/load are safe no-ops when no state dir
// is configured (in-memory-only mode).
func TestStatePersistenceNoDir(t *testing.T) {
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
