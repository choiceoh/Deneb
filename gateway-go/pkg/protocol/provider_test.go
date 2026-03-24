package protocol_test

import (
	"encoding/json"
	"testing"

	"github.com/choiceoh/deneb/gateway-go/pkg/protocol"
)

func TestProviderMetaJSON(t *testing.T) {
	meta := protocol.ProviderMeta{
		ID:      "anthropic",
		Label:   "Anthropic",
		Aliases: []string{"claude"},
		EnvVars: []string{"ANTHROPIC_API_KEY"},
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded protocol.ProviderMeta
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.ID != "anthropic" {
		t.Errorf("ID = %q, want %q", decoded.ID, "anthropic")
	}
	if len(decoded.Aliases) != 1 || decoded.Aliases[0] != "claude" {
		t.Errorf("Aliases = %v, want [claude]", decoded.Aliases)
	}
}

func TestProviderCatalogEntryJSON(t *testing.T) {
	label := "Claude Opus 4.6"
	ctx := int64(1000000)
	reasoning := true
	entry := protocol.ProviderCatalogEntry{
		Provider:      "anthropic",
		ModelID:       "claude-opus-4-6",
		Label:         &label,
		ContextWindow: &ctx,
		Reasoning:     &reasoning,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded protocol.ProviderCatalogEntry
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", decoded.Provider, "anthropic")
	}
	if decoded.ModelID != "claude-opus-4-6" {
		t.Errorf("ModelID = %q, want %q", decoded.ModelID, "claude-opus-4-6")
	}
	if decoded.Reasoning == nil || !*decoded.Reasoning {
		t.Errorf("Reasoning = %v, want true", decoded.Reasoning)
	}
}

func TestProviderCatalogSnapshotJSON(t *testing.T) {
	snapshot := protocol.ProviderCatalogSnapshot{
		Providers: []protocol.ProviderMeta{
			{ID: "anthropic", Label: "Anthropic"},
		},
		Entries: []protocol.ProviderCatalogEntry{
			{Provider: "anthropic", ModelID: "claude-opus-4-6"},
		},
		SnapshotAt: 1711234567890,
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded protocol.ProviderCatalogSnapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(decoded.Providers) != 1 {
		t.Fatalf("Providers length = %d, want 1", len(decoded.Providers))
	}
	if len(decoded.Entries) != 1 {
		t.Fatalf("Entries length = %d, want 1", len(decoded.Entries))
	}
}
