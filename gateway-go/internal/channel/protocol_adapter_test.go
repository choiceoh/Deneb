package channel_test

import (
	"context"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/internal/channel"
	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

// mockPlugin is a minimal channel plugin for testing.
type mockPlugin struct {
	id   string
	meta channel.Meta
	caps channel.Capabilities
	stat channel.Status
}

func (p *mockPlugin) ID() string                         { return p.id }
func (p *mockPlugin) Meta() channel.Meta                 { return p.meta }
func (p *mockPlugin) Capabilities() channel.Capabilities { return p.caps }
func (p *mockPlugin) Start(_ context.Context) error      { return nil }
func (p *mockPlugin) Stop(_ context.Context) error       { return nil }
func (p *mockPlugin) Status() channel.Status             { return p.stat }

func TestProtocolAdapterListPlugins(t *testing.T) {
	reg := channel.NewRegistry()
	if err := reg.Register(&mockPlugin{
		id:   "discord",
		meta: channel.Meta{ID: "discord", Label: "Discord"},
		stat: channel.Status{Connected: true},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	adapter := channel.NewProtocolAdapter(reg)
	plugins := adapter.ListPlugins()
	if len(plugins) != 1 {
		t.Fatalf("ListPlugins() returned %d, want 1", len(plugins))
	}
	if plugins[0].ID != "discord" {
		t.Errorf("ID = %q, want %q", plugins[0].ID, "discord")
	}
	if plugins[0].Kind != protocol.PluginKindChannel {
		t.Errorf("Kind = %q, want %q", plugins[0].Kind, protocol.PluginKindChannel)
	}
	if !plugins[0].Enabled {
		t.Errorf("Enabled = false, want true")
	}
}

func TestProtocolAdapterGetPluginHealth(t *testing.T) {
	reg := channel.NewRegistry()
	if err := reg.Register(&mockPlugin{
		id:   "telegram",
		meta: channel.Meta{ID: "telegram", Label: "Telegram"},
		stat: channel.Status{Connected: false, Error: "connection lost"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	adapter := channel.NewProtocolAdapter(reg)

	health := adapter.GetPluginHealth("telegram")
	if health == nil {
		t.Fatal("GetPluginHealth(telegram) returned nil")
	}
	if health.Healthy {
		t.Error("Healthy = true, want false")
	}
	if health.Error == nil || *health.Error != "connection lost" {
		t.Errorf("Error = %v, want %q", health.Error, "connection lost")
	}

	// Non-existent plugin.
	if h := adapter.GetPluginHealth("nonexistent"); h != nil {
		t.Errorf("GetPluginHealth(nonexistent) = %v, want nil", h)
	}
}
