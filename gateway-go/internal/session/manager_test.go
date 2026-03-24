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
