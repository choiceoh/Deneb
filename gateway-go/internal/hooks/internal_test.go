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
		t.Fatalf("expected 2 handlers called, got %d", len(order))
	}
	if order[0] != "type" {
		t.Errorf("expected type handler first, got %q", order[0])
	}
	if order[1] != "specific" {
		t.Errorf("expected specific handler second, got %q", order[1])
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
		t.Fatalf("expected 1, got %d", callCount)
	}

	if !reg.Unregister("command:new", "handler-a") {
		t.Error("expected unregister to return true")
	}

	reg.Trigger(context.Background(), event)
	if callCount != 1 {
		t.Errorf("expected still 1 after unregister, got %d", callCount)
	}
}

func TestInternalRegistry_NoHandlers(t *testing.T) {
	reg := NewInternalRegistry(nil)

	// Should not panic.
	event := &InternalHookEvent{Type: EventTypeGateway, Action: "startup"}
	reg.Trigger(context.Background(), event)
}

func TestEvaluateEligibility_AlwaysTrue(t *testing.T) {
	meta := &DenebHookMetadata{Always: true, OS: []string{"nonexistent"}}
	if !EvaluateEligibility(meta, EligibilityContext{Platform: "linux"}) {
		t.Error("always=true should bypass all checks")
	}
}

func TestEvaluateEligibility_OSFilter(t *testing.T) {
	meta := &DenebHookMetadata{OS: []string{"linux"}}
	if !EvaluateEligibility(meta, EligibilityContext{Platform: "linux"}) {
		t.Error("linux-only hook should pass on linux")
	}
	meta = &DenebHookMetadata{OS: []string{"freebsd"}}
	if EvaluateEligibility(meta, EligibilityContext{Platform: "linux"}) {
		t.Error("freebsd-only hook should fail on linux")
	}
}

func TestEvaluateEligibility_RequiredBins(t *testing.T) {
	meta := &DenebHookMetadata{
		Requires: &HookRequires{Bins: []string{"curl", "missing-bin"}},
	}
	ectx := EligibilityContext{
		Platform: "linux",
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
		Platform:  "linux",
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
		Platform:  "linux",
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
		t.Errorf("expected 'message:received', got %q", event.EventKey())
	}
}

func TestListHandlers(t *testing.T) {
	reg := NewInternalRegistry(nil)
	reg.Register("command", "a", func(ctx context.Context, evt *InternalHookEvent) error { return nil })
	reg.Register("command:new", "b", func(ctx context.Context, evt *InternalHookEvent) error { return nil })

	handlers := reg.ListHandlers()
	if len(handlers) != 2 {
		t.Errorf("expected 2 event keys, got %d", len(handlers))
	}
	if len(handlers["command"]) != 1 || handlers["command"][0] != "a" {
		t.Error("expected handler 'a' for 'command'")
	}
}
