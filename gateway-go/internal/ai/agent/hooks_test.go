package agent

import "testing"

func TestHookCompositorFansOutThinkingBreak(t *testing.T) {
	// The seam observer composes like every other hook: all registered handlers
	// fire, in registration order, and an unregistered hook stays nil so the
	// executor can skip it without a wrapper call per turn.
	var c HookCompositor
	if got := c.Build(); got.OnThinkingBreak != nil {
		t.Fatal("OnThinkingBreak should be nil with no handler registered")
	}

	order := []string{}
	c.OnThinkingBreak(func() { order = append(order, "first") })
	c.OnThinkingBreak(func() { order = append(order, "second") })
	hooks := c.Build()
	if hooks.OnThinkingBreak == nil {
		t.Fatal("OnThinkingBreak is nil after registering handlers")
	}
	hooks.OnThinkingBreak()
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("handlers fired %v, want [first second]", order)
	}
}
