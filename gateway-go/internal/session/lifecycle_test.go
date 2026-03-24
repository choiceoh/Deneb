package session

import "testing"

func TestDeriveStart(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseStart, Ts: 1000}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.Status != StatusRunning {
		t.Errorf("Status = %q, want %q", snap.Status, StatusRunning)
	}
	if snap.StartedAt == nil || *snap.StartedAt != 1000 {
		t.Error("StartedAt should be 1000")
	}
}

func TestDeriveEndDone(t *testing.T) {
	existing := &Session{StartedAt: int64Ptr(1000)}
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 2000}
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.Status != StatusDone {
		t.Errorf("Status = %q, want %q", snap.Status, StatusDone)
	}
	if snap.RuntimeMs == nil || *snap.RuntimeMs != 1000 {
		t.Errorf("RuntimeMs = %v, want 1000", snap.RuntimeMs)
	}
}

func TestDeriveEndKilled(t *testing.T) {
	existing := &Session{StartedAt: int64Ptr(1000)}
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 1500, StopReason: "aborted"}
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.Status != StatusKilled {
		t.Errorf("Status = %q, want %q", snap.Status, StatusKilled)
	}
}

func TestDeriveEndTimeout(t *testing.T) {
	existing := &Session{StartedAt: int64Ptr(1000)}
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 5000, Aborted: true}
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.Status != StatusTimeout {
		t.Errorf("Status = %q, want %q", snap.Status, StatusTimeout)
	}
}

func TestDeriveError(t *testing.T) {
	existing := &Session{StartedAt: int64Ptr(1000)}
	event := LifecycleEvent{Phase: PhaseError, Ts: 1200}
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.Status != StatusFailed {
		t.Errorf("Status = %q, want %q", snap.Status, StatusFailed)
	}
}

func int64Ptr(v int64) *int64 { return &v }
