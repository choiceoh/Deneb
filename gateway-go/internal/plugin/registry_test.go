package plugin

import (
	"context"
	"testing"
)

func TestFullRegistry_Channels(t *testing.T) {
	r := NewFullRegistry(testLogger())
	err := r.RegisterChannel(ChannelRegistration{
		PluginID:  "telegram-ext",
		ChannelID: "telegram",
		Label:     "Telegram",
	})
	if err != nil {
		t.Fatal(err)
	}
	ch := r.GetChannel("telegram")
	if ch == nil || ch.ChannelID != "telegram" {
		t.Error("expected telegram channel")
	}
	if len(r.ListChannels()) != 1 {
		t.Error("expected 1 channel")
	}
}

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
	r.RegisterPlugin(PluginMeta{ID: "test", Kind: KindChannel, Enabled: true})
	r.RegisterChannel(ChannelRegistration{PluginID: "test", ChannelID: "telegram"})
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
	if summary.Channels != 1 {
		t.Errorf("channels = %d", summary.Channels)
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
