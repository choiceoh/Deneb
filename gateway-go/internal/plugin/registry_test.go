package plugin

import (
	"context"
	"testing"
)

func TestFullRegistry_Providers(t *testing.T) {
	r := NewFullRegistry(testLogger())
	r.RegisterProvider(ProviderRegistration{
		PluginID:   "anthropic-ext",
		ProviderID: "anthropic",
		Config:     ProviderConfig{ID: "anthropic", Label: "Anthropic", Default: true},
	})
	p := r.GetProvider("anthropic")
	if p == nil || p.ProviderID != "anthropic" {
		t.Error("expected anthropic provider")
	}
}

func TestFullRegistry_Tools(t *testing.T) {
	r := NewFullRegistry(testLogger())
	r.RegisterTool(ToolRegistration{
		PluginID:   "core",
		Definition: ToolDefinition{Name: "bash", Description: "Run bash"},
		Handler: func(_ context.Context, _ map[string]any) (string, error) {
			return "ok", nil
		},
	})
	tool := r.GetTool("bash")
	if tool == nil || tool.Definition.Name != "bash" {
		t.Error("expected bash tool")
	}
}

func TestFullRegistry_HTTPRouteConflict(t *testing.T) {
	r := NewFullRegistry(testLogger())
	r.RegisterHTTPRoute(HTTPRouteRegistration{PluginID: "a", Method: "GET", Path: "/api/test"})
	err := r.RegisterHTTPRoute(HTTPRouteRegistration{PluginID: "b", Method: "GET", Path: "/api/test"})
	if err == nil {
		t.Error("expected conflict error")
	}
}

func TestFullRegistry_Summary(t *testing.T) {
	r := NewFullRegistry(testLogger())
	r.RegisterPlugin(PluginMeta{ID: "test", Kind: KindProvider, Enabled: true})
	r.RegisterProvider(ProviderRegistration{PluginID: "test", ProviderID: "anthropic"})
	r.RegisterTool(ToolRegistration{
		PluginID:   "test",
		Definition: ToolDefinition{Name: "bash"},
		Handler:    func(_ context.Context, _ map[string]any) (string, error) { return "", nil },
	})
	r.RegisterHook(HookBeforeAgentStart, "test", func(_ context.Context, _ map[string]any) error { return nil }, HookOptions{})

	summary := r.Summary()
	if summary.Plugins != 1 {
		t.Errorf("plugins = %d", summary.Plugins)
	}
	if summary.Providers != 1 {
		t.Errorf("providers = %d", summary.Providers)
	}
	if summary.Tools != 1 {
		t.Errorf("tools = %d", summary.Tools)
	}
	if summary.Hooks != 1 {
		t.Errorf("hooks = %d", summary.Hooks)
	}
}
