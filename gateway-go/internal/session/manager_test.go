package session

import "testing"

func TestCreateAndGet(t *testing.T) {
	m := NewManager()
	s := m.Create("session-1", KindDirect)
	if s.Key != "session-1" {
		t.Errorf("Key = %q, want %q", s.Key, "session-1")
	}
	if s.Kind != KindDirect {
		t.Errorf("Kind = %q, want %q", s.Kind, KindDirect)
	}

	got := m.Get("session-1")
	if got == nil {
		t.Fatal("session not found")
	}
}

func TestGetNotFound(t *testing.T) {
	m := NewManager()
	if m.Get("nonexistent") != nil {
		t.Error("should not find nonexistent session")
	}
}

func TestSetAndCount(t *testing.T) {
	m := NewManager()
	m.Set(&Session{Key: "s1", Kind: KindDirect})
	m.Set(&Session{Key: "s2", Kind: KindGroup})
	if m.Count() != 2 {
		t.Errorf("Count = %d, want 2", m.Count())
	}
}

func TestDelete(t *testing.T) {
	m := NewManager()
	m.Set(&Session{Key: "s1", Kind: KindDirect})
	ok := m.Delete("s1")
	if !ok {
		t.Error("Delete should return true for existing session")
	}
	ok = m.Delete("s1")
	if ok {
		t.Error("Delete should return false for nonexistent session")
	}
}

func TestApplyLifecycleEvent(t *testing.T) {
	m := NewManager()

	s := m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 1000})
	if s.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", s.Status, StatusRunning)
	}

	s = m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseEnd, Ts: 2000})
	if s.Status != StatusDone {
		t.Errorf("Status = %q, want %q", s.Status, StatusDone)
	}
	if s.RuntimeMs == nil || *s.RuntimeMs != 1000 {
		t.Errorf("RuntimeMs = %v, want 1000", s.RuntimeMs)
	}
}

func TestApplyLifecycleEventRestart(t *testing.T) {
	m := NewManager()
	m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 1000})
	m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseEnd, Ts: 2000})

	s := m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 3000})
	if s.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", s.Status, StatusRunning)
	}
	if s.EndedAt != nil {
		t.Error("EndedAt should be nil after restart")
	}
}

func TestConcurrentLifecycleEvents(t *testing.T) {
	m := NewManager()
	const n = 100

	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func(i int) {
			key := "concurrent-session"
			m.ApplyLifecycleEvent(key, LifecycleEvent{Phase: PhaseStart, Ts: int64(i * 1000)})
			m.ApplyLifecycleEvent(key, LifecycleEvent{Phase: PhaseEnd, Ts: int64(i*1000 + 500)})
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < n; i++ {
		<-done
	}

	s := m.Get("concurrent-session")
	if s == nil {
		t.Fatal("session should exist")
	}
	// Final state should be a valid terminal status.
	switch s.Status {
	case StatusDone, StatusRunning:
		// OK — depends on goroutine scheduling
	default:
		t.Errorf("unexpected status: %q", s.Status)
	}
}

func TestApplyLifecycleEndWithoutStart(t *testing.T) {
	m := NewManager()
	s := m.ApplyLifecycleEvent("no-start", LifecycleEvent{Phase: PhaseEnd, Ts: 5000})
	if s.Status != StatusDone {
		t.Errorf("Status = %q, want %q", s.Status, StatusDone)
	}
	// Without a prior start, startedAt falls back to event.Ts (same as endedAt),
	// so runtimeMs = endedAt - startedAt = 0.
	if s.RuntimeMs == nil || *s.RuntimeMs != 0 {
		t.Errorf("RuntimeMs = %v, want 0 (startedAt falls back to event.Ts)", s.RuntimeMs)
	}
}

func TestApplyLifecycleAbortedLastRun(t *testing.T) {
	m := NewManager()

	// Start: AbortedLastRun = false.
	s := m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 1000})
	if s.AbortedLastRun {
		t.Error("AbortedLastRun should be false after start")
	}

	// Killed: AbortedLastRun = true.
	s = m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseEnd, Ts: 2000, StopReason: "aborted"})
	if !s.AbortedLastRun {
		t.Error("AbortedLastRun should be true after killed")
	}

	// Restart: AbortedLastRun resets to false.
	s = m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 3000})
	if s.AbortedLastRun {
		t.Error("AbortedLastRun should be false after restart")
	}
}

func TestApplyLifecycleUpdatedAtFromSnapshot(t *testing.T) {
	m := NewManager()
	s := m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 4200})
	if s.UpdatedAt != 4200 {
		t.Errorf("UpdatedAt = %d, want 4200 (from snapshot)", s.UpdatedAt)
	}
}

func TestApplyLifecycleUnknownPhase(t *testing.T) {
	m := NewManager()

	// Apply a valid event first.
	m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 1000})

	// Unknown phase should not mutate the session.
	s := m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: "bogus", Ts: 9999})
	if s.Status != StatusRunning {
		t.Errorf("Status = %q, want %q (unchanged after unknown phase)", s.Status, StatusRunning)
	}
}

func TestApplyLifecycleReturnsSnapshotCopy(t *testing.T) {
	m := NewManager()
	s := m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseStart, Ts: 1000})

	// Mutate the returned snapshot — should not affect internal state.
	s.Status = StatusFailed
	s.AbortedLastRun = true

	internal := m.Get("s1")
	if internal.Status != StatusRunning {
		t.Errorf("internal Status = %q, want %q (mutation leaked)", internal.Status, StatusRunning)
	}
	if internal.AbortedLastRun {
		t.Error("internal AbortedLastRun should be false (mutation leaked)")
	}
}

func TestCreateReturnsSnapshotCopy(t *testing.T) {
	m := NewManager()
	s := m.Create("s1", KindDirect)

	// Mutate the returned snapshot — should not affect internal state.
	s.Kind = KindGroup

	internal := m.Get("s1")
	if internal.Kind != KindDirect {
		t.Errorf("internal Kind = %q, want %q (mutation leaked)", internal.Kind, KindDirect)
	}
}

func TestDeriveSnapshotUpdatedAtNotAliased(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseStart, Ts: 3000}
	snap := DeriveLifecycleSnapshot(nil, event)

	if snap.StartedAt == nil || snap.UpdatedAt == nil {
		t.Fatal("both StartedAt and UpdatedAt should be set")
	}
	if snap.StartedAt == snap.UpdatedAt {
		t.Error("StartedAt and UpdatedAt should not be the same pointer (aliased)")
	}
	if *snap.StartedAt != *snap.UpdatedAt {
		t.Errorf("values should be equal: StartedAt=%d, UpdatedAt=%d", *snap.StartedAt, *snap.UpdatedAt)
	}
}

func TestApplyLifecycleRuntimeMsFallback(t *testing.T) {
	m := NewManager()

	// Manually set a session with existing runtimeMs but no startedAt.
	rm := int64(777)
	m.Set(&Session{Key: "s1", Kind: KindDirect, RuntimeMs: &rm})

	// End event with Ts=0 means endedAt won't resolve, but existing runtimeMs preserved.
	s := m.ApplyLifecycleEvent("s1", LifecycleEvent{Phase: PhaseEnd, Ts: 0})
	if s.RuntimeMs == nil || *s.RuntimeMs != 777 {
		t.Errorf("RuntimeMs = %v, want 777 (fallback to existing)", s.RuntimeMs)
	}
}
