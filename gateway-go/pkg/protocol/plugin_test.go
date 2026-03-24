package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestPluginMetaJSON(t *testing.T) {
	desc := "A test plugin"
	meta := protocol.PluginMeta{
		ID:          "test-plugin",
		Name:        "Test Plugin",
		Kind:        protocol.PluginKindChannel,
		Version:     "1.0.0",
		Enabled:     true,
		Description: &desc,
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded protocol.PluginMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != "test-plugin" {
		t.Errorf("ID = %q, want %q", decoded.ID, "test-plugin")
	}
	if decoded.Kind != protocol.PluginKindChannel {
		t.Errorf("Kind = %q, want %q", decoded.Kind, protocol.PluginKindChannel)
	}
	if decoded.Description == nil || *decoded.Description != "A test plugin" {
		t.Errorf("Description = %v, want %q", decoded.Description, "A test plugin")
	}
	if decoded.Source != nil {
		t.Errorf("Source should be nil, got %v", decoded.Source)
	}
}

func TestPluginRegistrySnapshotJSON(t *testing.T) {
	snapshot := protocol.PluginRegistrySnapshot{
		Plugins: []protocol.PluginMeta{
			{ID: "discord", Name: "Discord", Kind: protocol.PluginKindChannel, Version: "1.0.0", Enabled: true},
		},
		Health: []protocol.PluginHealthStatus{
			{PluginID: "discord", Healthy: true},
		},
		SnapshotAt: 1711234567890,
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded protocol.PluginRegistrySnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Plugins) != 1 {
		t.Fatalf("Plugins length = %d, want 1", len(decoded.Plugins))
	}
	if decoded.Plugins[0].ID != "discord" {
		t.Errorf("Plugins[0].ID = %q, want %q", decoded.Plugins[0].ID, "discord")
	}
	if len(decoded.Health) != 1 || !decoded.Health[0].Healthy {
		t.Errorf("Health[0].Healthy = %v, want true", decoded.Health[0].Healthy)
	}
}
