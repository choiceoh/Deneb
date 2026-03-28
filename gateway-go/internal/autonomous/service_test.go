package autonomous

import (
	"testing"
)

func TestNewService_notNil(t *testing.T) {
	s := NewService(nil)
	if s == nil {
		t.Fatal("NewService returned nil")
	}
}

func TestNewService_nilLogger(t *testing.T) {
	// Should not panic with nil logger — uses slog.Default() as fallback.
	s := NewService(nil)
	if s.logger == nil {
		t.Fatal("expected non-nil logger after nil fallback")
	}
}

func TestService_StartStop(t *testing.T) {
	s := NewService(nil)
	s.Start()
	s.Stop() // must not panic or deadlock
}

func TestService_StopIdempotent(t *testing.T) {
	s := NewService(nil)
	s.Start()
	s.Stop()
	s.Stop() // second Stop must not panic
}

func TestService_OnEvent_listenerRegistered(t *testing.T) {
	s := NewService(nil)
	called := false
	s.OnEvent(func(event CycleEvent) {
		called = true
	})
	// Directly emit to verify the listener was registered.
	s.emit(CycleEvent{Type: "test"})
	if !called {
		t.Fatal("listener was not called after emit")
	}
}

func TestService_OnEvent_multipleListeners(t *testing.T) {
	s := NewService(nil)
	count := 0
	s.OnEvent(func(CycleEvent) { count++ })
	s.OnEvent(func(CycleEvent) { count++ })
	s.emit(CycleEvent{Type: "test"})
	if count != 2 {
		t.Errorf("expected 2 listeners called, got %d", count)
	}
}

func TestService_IncrementDreamTurn_noDreamer(t *testing.T) {
	s := NewService(nil)
	// Must not panic when dreamer is not configured.
	s.IncrementDreamTurn(nil) //nolint:staticcheck // nil context is intentional for no-op path
}
