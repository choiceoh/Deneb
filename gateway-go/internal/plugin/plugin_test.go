package plugin

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestRegistryBasic(t *testing.T) {
	r := NewRegistry(testLogger())

	err := r.Register(PluginMeta{ID: "telegram", Kind: KindChannel, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	err = r.Register(PluginMeta{ID: "anthropic", Kind: KindProvider, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("get", func(t *testing.T) {
		p := r.Get("telegram")
		if p == nil || p.ID != "telegram" {
			t.Error("expected telegram plugin")
		}
	})

	t.Run("get missing", func(t *testing.T) {
		if r.Get("unknown") != nil {
			t.Error("expected nil for unknown")
		}
	})

	t.Run("list", func(t *testing.T) {
		all := r.List()
		if len(all) != 2 {
			t.Errorf("expected 2 plugins, got %d", len(all))
		}
	})

	t.Run("list by kind", func(t *testing.T) {
		channels := r.ListByKind(KindChannel)
		if len(channels) != 1 {
			t.Errorf("expected 1 channel, got %d", len(channels))
		}
	})

	t.Run("is enabled", func(t *testing.T) {
		if !r.IsEnabled("telegram") {
			t.Error("expected telegram enabled")
		}
	})

	t.Run("empty id error", func(t *testing.T) {
		err := r.Register(PluginMeta{Kind: KindChannel})
		if err == nil {
			t.Error("expected error for empty ID")
		}
	})
}

func TestHookSystem(t *testing.T) {
	r := NewRegistry(testLogger())

	var called []string
	r.RegisterHook(HookBeforeAgentStart, "test", func(_ context.Context, _ map[string]any) error {
		called = append(called, "hook1")
		return nil
	})
	r.RegisterHook(HookBeforeAgentStart, "test2", func(_ context.Context, _ map[string]any) error {
		called = append(called, "hook2")
		return nil
	})
	r.RegisterHook(HookAfterAgentEnd, "test", func(_ context.Context, _ map[string]any) error {
		called = append(called, "hook3")
		return nil
	})

	errs := r.RunHooks(context.Background(), HookBeforeAgentStart, nil)
	if len(errs) != 0 {
		t.Errorf("expected no errors, got %v", errs)
	}
	if len(called) != 2 {
		t.Errorf("expected 2 hooks called, got %d", len(called))
	}
}

func TestProviderCatalog(t *testing.T) {
	c := NewProviderCatalog()

	c.Register(ProviderConfig{ID: "anthropic", Label: "Anthropic", Default: true})
	c.Register(ProviderConfig{ID: "openai", Label: "OpenAI"})

	t.Run("get", func(t *testing.T) {
		p := c.Get("anthropic")
		if p == nil || p.ID != "anthropic" {
			t.Error("expected anthropic")
		}
	})

	t.Run("default", func(t *testing.T) {
		p := c.Default()
		if p == nil || p.ID != "anthropic" {
			t.Error("expected anthropic as default")
		}
	})

	t.Run("list", func(t *testing.T) {
		all := c.List()
		if len(all) != 2 {
			t.Errorf("expected 2, got %d", len(all))
		}
	})

	t.Run("get missing", func(t *testing.T) {
		if c.Get("unknown") != nil {
			t.Error("expected nil")
		}
	})
}
