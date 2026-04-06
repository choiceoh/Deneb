package provider

import (
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

func (p *testPlugin) ID() string                { return p.id }
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

func TestGetByNormalizedIDWithAliases(t *testing.T) {
	reg := NewRegistry()
	tp := &testPlugin{
		id:      "openai",
		label:   "OpenAI",
		aliases: []string{"oai"},
	}
	reg.Register(tp)

	tests := []struct {
		providerID string
		wantFound  bool
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
		got := reg.GetByNormalizedID(tt.providerID)
		found := got != nil
		if found != tt.wantFound {
			t.Errorf("GetByNormalizedID(%q) found=%v, want %v", tt.providerID, found, tt.wantFound)
		}
	}
}
