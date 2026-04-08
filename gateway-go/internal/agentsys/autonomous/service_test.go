package autonomous

import (
	"testing"
)



func TestService_StartStop(t *testing.T) {
	s := NewService(nil)
	s.Start()
	s.Stop() // must not panic or deadlock
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
		t.Errorf("got %d, want 2 listeners called", count)
	}
}

