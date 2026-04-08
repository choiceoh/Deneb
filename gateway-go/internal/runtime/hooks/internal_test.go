package hooks

import (
	"context"
	"testing"
	"time"
)

func TestInternalRegistry_TwoLevelMatching(t *testing.T) {
	reg := NewInternalRegistry(nil)

	var order []string

	reg.Register("command", "type-handler", func(ctx context.Context, evt *InternalHookEvent) error {
		order = append(order, "type")
		return nil
	})
	reg.Register("command:new", "specific-handler", func(ctx context.Context, evt *InternalHookEvent) error {
		order = append(order, "specific")
		return nil
	})

	event := &InternalHookEvent{
		Type:       EventTypeCommand,
		Action:     "new",
		SessionKey: "test-session",
		Timestamp:  time.Now(),
	}
	reg.Trigger(context.Background(), event)

	if len(order) != 2 {
		t.Fatalf("got %d, want 2 handlers called", len(order))
	}
	if order[0] != "type" {
		t.Errorf("got %q, want type handler first", order[0])
	}
	if order[1] != "specific" {
		t.Errorf("got %q, want specific handler second", order[1])
	}
}

func TestInternalRegistry_ErrorIsolation(t *testing.T) {
	reg := NewInternalRegistry(nil)
	called := false

	// First handler panics.
	reg.Register("command:new", "panicking", func(ctx context.Context, evt *InternalHookEvent) error {
		panic("test panic")
	})

	// Second handler should still run.
	reg.Register("command:new", "survivor", func(ctx context.Context, evt *InternalHookEvent) error {
		called = true
		return nil
	})

	event := &InternalHookEvent{
		Type:   EventTypeCommand,
		Action: "new",
	}
	reg.Trigger(context.Background(), event)

	if !called {
		t.Error("survivor handler should have been called despite panic in first")
	}
}

func TestInternalRegistry_Unregister(t *testing.T) {
	reg := NewInternalRegistry(nil)
	callCount := 0

	reg.Register("command:new", "handler-a", func(ctx context.Context, evt *InternalHookEvent) error {
		callCount++
		return nil
	})

	event := &InternalHookEvent{Type: EventTypeCommand, Action: "new"}

	reg.Trigger(context.Background(), event)
	if callCount != 1 {
		t.Fatalf("got %d, want 1", callCount)
	}

	if !reg.Unregister("command:new", "handler-a") {
		t.Error("expected unregister to return true")
	}

	reg.Trigger(context.Background(), event)
	if callCount != 1 {
		t.Errorf("got %d, want still 1 after unregister", callCount)
	}
}

func TestInternalRegistry_NoHandlers(t *testing.T) {
	reg := NewInternalRegistry(nil)

	// Should not panic.
	event := &InternalHookEvent{Type: EventTypeGateway, Action: "startup"}
	reg.Trigger(context.Background(), event)
}

func TestEvaluateEligibility_AlwaysTrue(t *testing.T) {
	meta := &DenebHookMetadata{Always: true}
	if !EvaluateEligibility(meta, EligibilityContext{}) {
		t.Error("always=true should bypass all checks")
	}
}

func TestEvaluateEligibility_RequiredBins(t *testing.T) {
	meta := &DenebHookMetadata{
		Requires: &HookRequires{Bins: []string{"curl", "missing-bin"}},
	}
	ectx := EligibilityContext{
		BinLookup: func(name string) bool {
			return name == "curl"
		},
	}
	if EvaluateEligibility(meta, ectx) {
		t.Error("should fail when required bin is missing")
	}
}

func TestEvaluateEligibility_AnyBins(t *testing.T) {
	meta := &DenebHookMetadata{
		Requires: &HookRequires{AnyBins: []string{"bash", "zsh"}},
	}
	ectx := EligibilityContext{
		BinLookup: func(name string) bool { return name == "zsh" },
	}
	if !EvaluateEligibility(meta, ectx) {
		t.Error("should pass when at least one anyBin is available")
	}
}

func TestEvaluateEligibility_RequiredEnv(t *testing.T) {
	meta := &DenebHookMetadata{
		Requires: &HookRequires{Env: []string{"MY_API_KEY"}},
	}
	ectx := EligibilityContext{
		EnvLookup: func(name string) string { return "" },
	}
	if EvaluateEligibility(meta, ectx) {
		t.Error("should fail when env var is empty")
	}

	ectx.EnvLookup = func(name string) string { return "set" }
	if !EvaluateEligibility(meta, ectx) {
		t.Error("should pass when env var is set")
	}
}

func TestEvaluateEligibility_NilMetadata(t *testing.T) {
	if !EvaluateEligibility(nil, EligibilityContext{}) {
		t.Error("nil metadata should be eligible")
	}
}

func TestEventKey(t *testing.T) {
	event := &InternalHookEvent{Type: EventTypeMessage, Action: "received"}
	if event.EventKey() != "message:received" {
		t.Errorf("got %q, want 'message:received'", event.EventKey())
	}
}

func TestListHandlers(t *testing.T) {
	reg := NewInternalRegistry(nil)
	reg.Register("command", "a", func(ctx context.Context, evt *InternalHookEvent) error { return nil })
	reg.Register("command:new", "b", func(ctx context.Context, evt *InternalHookEvent) error { return nil })

	handlers := reg.ListHandlers()
	if len(handlers) != 2 {
		t.Errorf("got %d, want 2 event keys", len(handlers))
	}
	if len(handlers["command"]) != 1 || handlers["command"][0] != "a" {
		t.Error("expected handler 'a' for 'command'")
	}
}
