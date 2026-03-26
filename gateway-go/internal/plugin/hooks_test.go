package plugin

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
)

func testHookLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestHookRunner_RegisterAndRun(t *testing.T) {
	r := NewHookRunner(testHookLogger())

	called := false
	r.Register(HookGatewayStart, "test-plugin", func(_ context.Context, _ map[string]any) error {
		called = true
		return nil
	}, HookOptions{})

	results := r.Run(context.Background(), HookGatewayStart, nil)
	if !called {
		t.Error("hook was not called")
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != nil {
		t.Errorf("unexpected error: %v", results[0].Error)
	}
	if results[0].PluginID != "test-plugin" {
		t.Errorf("expected pluginID 'test-plugin', got %q", results[0].PluginID)
	}
}

func TestHookRunner_PriorityOrder(t *testing.T) {
	r := NewHookRunner(testHookLogger())

	var order []string
	r.Register(HookGatewayStart, "late", func(_ context.Context, _ map[string]any) error {
		order = append(order, "late")
		return nil
	}, HookOptions{Priority: HookPriorityLate})

	r.Register(HookGatewayStart, "early", func(_ context.Context, _ map[string]any) error {
		order = append(order, "early")
		return nil
	}, HookOptions{Priority: HookPriorityEarly})

	r.Register(HookGatewayStart, "normal", func(_ context.Context, _ map[string]any) error {
		order = append(order, "normal")
		return nil
	}, HookOptions{Priority: HookPriorityNormal})

	r.Run(context.Background(), HookGatewayStart, nil)

	if len(order) != 3 {
		t.Fatalf("expected 3 hooks, got %d", len(order))
	}
	if order[0] != "early" || order[1] != "normal" || order[2] != "late" {
		t.Errorf("wrong execution order: %v", order)
	}
}

func TestHookRunner_ErrorHandling(t *testing.T) {
	r := NewHookRunner(testHookLogger())

	expectedErr := errors.New("hook failed")
	r.Register(HookGatewayStop, "failing", func(_ context.Context, _ map[string]any) error {
		return expectedErr
	}, HookOptions{})

	results := r.Run(context.Background(), HookGatewayStop, nil)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Error != expectedErr {
		t.Errorf("expected error %v, got %v", expectedErr, results[0].Error)
	}
}

func TestHookRunner_NoMatchingHooks(t *testing.T) {
	r := NewHookRunner(testHookLogger())

	r.Register(HookGatewayStart, "test", func(_ context.Context, _ map[string]any) error {
		return nil
	}, HookOptions{})

	results := r.Run(context.Background(), HookGatewayStop, nil)
	if results != nil {
		t.Errorf("expected nil for no matching hooks, got %v", results)
	}
}

func TestHookRunner_Count(t *testing.T) {
	r := NewHookRunner(testHookLogger())

	if r.Count(HookGatewayStart) != 0 {
		t.Error("expected count 0 initially")
	}

	r.Register(HookGatewayStart, "a", func(_ context.Context, _ map[string]any) error { return nil }, HookOptions{})
	r.Register(HookGatewayStart, "b", func(_ context.Context, _ map[string]any) error { return nil }, HookOptions{})
	r.Register(HookGatewayStop, "c", func(_ context.Context, _ map[string]any) error { return nil }, HookOptions{})

	if r.Count(HookGatewayStart) != 2 {
		t.Errorf("expected count 2, got %d", r.Count(HookGatewayStart))
	}
	if r.Count(HookGatewayStop) != 1 {
		t.Errorf("expected count 1, got %d", r.Count(HookGatewayStop))
	}
}

func TestHookRunner_ListHookNames(t *testing.T) {
	r := NewHookRunner(testHookLogger())

	r.Register(HookGatewayStart, "a", func(_ context.Context, _ map[string]any) error { return nil }, HookOptions{})
	r.Register(HookGatewayStart, "b", func(_ context.Context, _ map[string]any) error { return nil }, HookOptions{})
	r.Register(HookGatewayStop, "c", func(_ context.Context, _ map[string]any) error { return nil }, HookOptions{})

	names := r.ListHookNames()
	if len(names) != 2 {
		t.Fatalf("expected 2 unique hook names, got %d", len(names))
	}
}

func TestValidateHookName(t *testing.T) {
	// Valid names.
	validNames := []HookName{
		HookGatewayStart, HookGatewayStop, HookBeforeModelResolve,
		HookLLMInput, HookLLMOutput, HookAgentEnd,
		HookBeforeCompaction, HookAfterCompaction,
		HookBeforeToolCall, HookAfterToolCall,
		HookSessionStart, HookSessionEnd,
		HookSubagentSpawning, HookSubagentSpawned, HookSubagentEnded,
	}
	for _, name := range validNames {
		if err := ValidateHookName(name); err != nil {
			t.Errorf("ValidateHookName(%q) unexpected error: %v", name, err)
		}
	}

	// Invalid names.
	if err := ValidateHookName("nonexistent_hook"); err == nil {
		t.Error("expected error for unknown hook name")
	}
	if err := ValidateHookName(""); err == nil {
		t.Error("expected error for empty hook name")
	}
}

func TestSortHooksByPriority(t *testing.T) {
	hooks := []registeredHook{
		{entry: HookEntry{PluginID: "c"}, options: HookOptions{Priority: HookPriorityLate}},
		{entry: HookEntry{PluginID: "a"}, options: HookOptions{Priority: HookPriorityEarly}},
		{entry: HookEntry{PluginID: "b"}, options: HookOptions{Priority: HookPriorityNormal}},
	}

	sortHooksByPriority(hooks)

	if hooks[0].entry.PluginID != "a" {
		t.Errorf("first hook should be 'a' (early), got %q", hooks[0].entry.PluginID)
	}
	if hooks[1].entry.PluginID != "b" {
		t.Errorf("second hook should be 'b' (normal), got %q", hooks[1].entry.PluginID)
	}
	if hooks[2].entry.PluginID != "c" {
		t.Errorf("third hook should be 'c' (late), got %q", hooks[2].entry.PluginID)
	}
}

func TestSortHooksByPriority_Empty(t *testing.T) {
	var hooks []registeredHook
	sortHooksByPriority(hooks) // Should not panic.
}

func TestSortHooksByPriority_Single(t *testing.T) {
	hooks := []registeredHook{
		{entry: HookEntry{PluginID: "only"}, options: HookOptions{Priority: HookPriorityNormal}},
	}
	sortHooksByPriority(hooks)
	if hooks[0].entry.PluginID != "only" {
		t.Error("single element should remain unchanged")
	}
}
