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
		t.Errorf("got %q, want ID 'openai'", p.ID())
	}

	// Cache hit.
	p2 := resolver.ResolvePlugin("openai")
	if p2 != p {
		t.Error("expected cached plugin on second call")
	}

	// Not found.
	p3 := resolver.ResolvePlugin("nonexistent")
	if p3 != nil {
		t.Errorf("got %v, want nil for nonexistent provider", p3)
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
		t.Errorf("got %v, want nil capabilities for nonexistent", c2)
	}
}
