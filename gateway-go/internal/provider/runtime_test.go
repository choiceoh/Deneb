package provider

import (
	"context"
	"log/slog"
	"testing"
)

// testPlugin implements the Plugin interface for testing.
type testPlugin struct {
	id      string
	label   string
	auth    []AuthMethod
	aliases []string
	caps    *Capabilities
}

func (p *testPlugin) ID() string               { return p.id }
func (p *testPlugin) Label() string             { return p.label }
func (p *testPlugin) AuthMethods() []AuthMethod { return p.auth }
func (p *testPlugin) Aliases() []string         { return p.aliases }
func (p *testPlugin) Capabilities() Capabilities {
	if p.caps != nil {
		return *p.caps
	}
	return Capabilities{}
}

func TestProviderRuntimeResolverResolvePlugin(t *testing.T) {
	reg := NewRegistry()
	tp := &testPlugin{id: "openai", label: "OpenAI"}
	reg.Register(tp)

	resolver := NewProviderRuntimeResolver(reg, slog.Default())

	// Direct lookup.
	p := resolver.ResolvePlugin("openai")
	if p == nil {
		t.Fatal("expected plugin for 'openai'")
	}
	if p.ID() != "openai" {
		t.Errorf("expected ID 'openai', got %q", p.ID())
	}

	// Cache hit.
	p2 := resolver.ResolvePlugin("openai")
	if p2 != p {
		t.Error("expected cached plugin on second call")
	}

	// Not found.
	p3 := resolver.ResolvePlugin("nonexistent")
	if p3 != nil {
		t.Errorf("expected nil for nonexistent provider, got %v", p3)
	}
}

func TestProviderRuntimeResolverWithAliases(t *testing.T) {
	reg := NewRegistry()
	tp := &testPlugin{
		id:      "amazon-bedrock",
		label:   "Amazon Bedrock",
		aliases: []string{"bedrock", "aws-bedrock"},
	}
	reg.Register(tp)

	resolver := NewProviderRuntimeResolver(reg, slog.Default())

	// Lookup by alias.
	p := resolver.ResolvePlugin("bedrock")
	if p == nil {
		t.Fatal("expected plugin for alias 'bedrock'")
	}
	if p.ID() != "amazon-bedrock" {
		t.Errorf("expected ID 'amazon-bedrock', got %q", p.ID())
	}

	// Lookup by normalized alias.
	p2 := resolver.ResolvePlugin("aws-bedrock")
	if p2 == nil {
		t.Fatal("expected plugin for alias 'aws-bedrock'")
	}
}

func TestProviderRuntimeResolverResetCache(t *testing.T) {
	reg := NewRegistry()
	tp := &testPlugin{id: "openai", label: "OpenAI"}
	reg.Register(tp)

	resolver := NewProviderRuntimeResolver(reg, slog.Default())

	resolver.ResolvePlugin("openai")
	resolver.ResetCache()

	// Should not be in cache anymore, but still findable.
	p := resolver.ResolvePlugin("openai")
	if p == nil {
		t.Fatal("expected plugin after cache reset")
	}
}

func TestProviderRuntimeResolverCapabilities(t *testing.T) {
	reg := NewRegistry()
	caps := &Capabilities{SupportsStreaming: true, SupportsTools: true}
	tp := &testPlugin{id: "openai", label: "OpenAI", caps: caps}
	reg.Register(tp)

	resolver := NewProviderRuntimeResolver(reg, slog.Default())

	c := resolver.ResolveCapabilities("openai")
	if c == nil {
		t.Fatal("expected capabilities")
	}
	if !c.SupportsStreaming {
		t.Error("expected SupportsStreaming = true")
	}
	if !c.SupportsTools {
		t.Error("expected SupportsTools = true")
	}

	// Non-existent provider.
	c2 := resolver.ResolveCapabilities("nonexistent")
	if c2 != nil {
		t.Errorf("expected nil capabilities for nonexistent, got %v", c2)
	}
}

func TestMatchesProviderID(t *testing.T) {
	tp := &testPlugin{
		id:      "openai",
		label:   "OpenAI",
		aliases: []string{"oai"},
	}

	tests := []struct {
		providerID string
		want       bool
	}{
		{"openai", true},
		{"OpenAI", true},
		{"OPENAI", true},
		{"oai", true},
		{"OAI", true},
		{"anthropic", false},
		{"", false},
	}

	for _, tt := range tests {
		got := matchesProviderID(tp, tt.providerID)
		if got != tt.want {
			t.Errorf("matchesProviderID(openai, %q) = %v, want %v", tt.providerID, got, tt.want)
		}
	}
}

func TestProviderRuntimeResolverMissingAuth(t *testing.T) {
	reg := NewRegistry()
	tp := &testPlugin{id: "openai", label: "OpenAI"}
	reg.Register(tp)

	resolver := NewProviderRuntimeResolver(reg, slog.Default())

	// Provider exists but doesn't implement MissingAuthMessageProvider.
	msg := resolver.BuildMissingAuthMessage("openai")
	if msg != "" {
		t.Errorf("expected empty message, got %q", msg)
	}
}

func TestProviderRuntimeResolverModelSuppression(t *testing.T) {
	reg := NewRegistry()
	tp := &testPlugin{id: "openai", label: "OpenAI"}
	reg.Register(tp)

	resolver := NewProviderRuntimeResolver(reg, slog.Default())

	// No suppression provider → nil.
	result := resolver.ResolveBuiltInModelSuppression("openai", "gpt-4")
	if result != nil {
		t.Errorf("expected nil suppression, got %v", result)
	}
}

func TestProviderRuntimeResolverCatalogAugmentation(t *testing.T) {
	reg := NewRegistry()
	tp := &testPlugin{id: "openai", label: "OpenAI"}
	reg.Register(tp)

	resolver := NewProviderRuntimeResolver(reg, slog.Default())

	entries := []CatalogEntry{{Provider: "openai", ModelID: "gpt-4"}}
	extra := resolver.AugmentModelCatalog(context.Background(), entries)
	if len(extra) != 0 {
		t.Errorf("expected 0 augmented entries, got %d", len(extra))
	}
}
