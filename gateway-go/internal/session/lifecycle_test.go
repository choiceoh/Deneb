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

func TestDeriveUnknownPhase(t *testing.T) {
	event := LifecycleEvent{Phase: "bogus", Ts: 1000}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.Status != "" {
		t.Errorf("Status = %q, want empty for unknown phase", snap.Status)
	}
	if snap.StartedAt != nil || snap.EndedAt != nil || snap.RuntimeMs != nil || snap.UpdatedAt != nil {
		t.Error("all fields should be nil for unknown phase")
	}
}

func TestDeriveStartWithEventStartedAt(t *testing.T) {
	sa := int64(500)
	event := LifecycleEvent{Phase: PhaseStart, Ts: 1000, StartedAt: &sa}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.StartedAt == nil || *snap.StartedAt != 500 {
		t.Errorf("StartedAt = %v, want 500 (event.StartedAt takes priority)", snap.StartedAt)
	}
}

func TestDeriveStartWithExistingStartedAt(t *testing.T) {
	existing := &Session{StartedAt: int64Ptr(800)}
	event := LifecycleEvent{Phase: PhaseStart, Ts: 0} // Ts=0 is not valid
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.StartedAt == nil || *snap.StartedAt != 800 {
		t.Errorf("StartedAt = %v, want 800 (existing fallback)", snap.StartedAt)
	}
}

func TestDeriveStartWithZeroTs(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseStart, Ts: 0}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.StartedAt != nil {
		t.Errorf("StartedAt = %v, want nil when no valid timestamps", snap.StartedAt)
	}
}

func TestDeriveStartUpdatedAt(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseStart, Ts: 3000}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.UpdatedAt == nil || *snap.UpdatedAt != 3000 {
		t.Errorf("UpdatedAt = %v, want 3000 (equals resolved startedAt)", snap.UpdatedAt)
	}
}

func TestDeriveStartAbortedLastRun(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseStart, Ts: 1000}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.AbortedLastRun {
		t.Error("AbortedLastRun should be false for start phase")
	}
}

func TestDeriveEndWithEventEndedAt(t *testing.T) {
	ea := int64(1800)
	existing := &Session{StartedAt: int64Ptr(1000)}
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 2000, EndedAt: &ea}
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.EndedAt == nil || *snap.EndedAt != 1800 {
		t.Errorf("EndedAt = %v, want 1800 (event.EndedAt takes priority)", snap.EndedAt)
	}
	// RuntimeMs should use the event.EndedAt value.
	if snap.RuntimeMs == nil || *snap.RuntimeMs != 800 {
		t.Errorf("RuntimeMs = %v, want 800", snap.RuntimeMs)
	}
}

func TestDeriveEndStartedAtFallback(t *testing.T) {
	existing := &Session{StartedAt: int64Ptr(500)}
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 2000}
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.StartedAt == nil || *snap.StartedAt != 500 {
		t.Errorf("StartedAt = %v, want 500 (preserved from existing)", snap.StartedAt)
	}
	if snap.RuntimeMs == nil || *snap.RuntimeMs != 1500 {
		t.Errorf("RuntimeMs = %v, want 1500", snap.RuntimeMs)
	}
}

func TestDeriveEndRuntimeMsFallback(t *testing.T) {
	rm := int64(999)
	existing := &Session{RuntimeMs: &rm} // no StartedAt
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 0}
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.RuntimeMs == nil || *snap.RuntimeMs != 999 {
		t.Errorf("RuntimeMs = %v, want 999 (fallback to existing)", snap.RuntimeMs)
	}
}

func TestDeriveEndRuntimeMsClampsToZero(t *testing.T) {
	existing := &Session{StartedAt: int64Ptr(5000)}
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 1000} // endedAt < startedAt
	snap := DeriveLifecycleSnapshot(existing, event)
	if snap.RuntimeMs == nil || *snap.RuntimeMs != 0 {
		t.Errorf("RuntimeMs = %v, want 0 (clamped)", snap.RuntimeMs)
	}
}

func TestDeriveEndKilledAbortedLastRun(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 2000, StopReason: "aborted"}
	snap := DeriveLifecycleSnapshot(nil, event)
	if !snap.AbortedLastRun {
		t.Error("AbortedLastRun should be true when killed")
	}
}

func TestDeriveEndDoneAbortedLastRun(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseEnd, Ts: 2000}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.AbortedLastRun {
		t.Error("AbortedLastRun should be false when done")
	}
}

func TestDeriveErrorAbortedLastRun(t *testing.T) {
	event := LifecycleEvent{Phase: PhaseError, Ts: 2000}
	snap := DeriveLifecycleSnapshot(nil, event)
	if snap.AbortedLastRun {
		t.Error("AbortedLastRun should be false for error/failed")
	}
}

func TestIsFiniteTimestamp(t *testing.T) {
	tests := []struct {
		name string
		v    *int64
		want bool
	}{
		{"nil", nil, false},
		{"zero", int64Ptr(0), false},
		{"negative", int64Ptr(-1), false},
		{"positive", int64Ptr(1000), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isFiniteTimestamp(tt.v); got != tt.want {
				t.Errorf("isFiniteTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func int64Ptr(v int64) *int64 { return &v }
