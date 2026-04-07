package provider

import (
	"testing"
)

func TestNormalizeProviderID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"z.ai", "zai"},
		{"Z.AI", "zai"},
		{"z-ai", "zai"},
		{"opencode-zen", "opencode"},
		{"opencode-go-auth", "opencode-go"},
		{"qwen", "qwen-portal"},
		{"Qwen", "qwen-portal"},
		{"kimi", "kimi"},
		{"kimi-code", "kimi"},
		{"kimi-coding", "kimi"},
		{"bedrock", "amazon-bedrock"},
		{"aws-bedrock", "amazon-bedrock"},
		{"Bedrock", "amazon-bedrock"},
		{"bytedance", "volcengine"},
		{"doubao", "volcengine"},
		{"openai", "openai"},
		{"anthropic", "anthropic"},
		{"  OpenAI  ", "openai"},
	}

	for _, tt := range tests {
		result := NormalizeProviderID(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeProviderID(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestNormalizeProviderIDForAuth(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"volcengine-plan", "volcengine"},
		{"byteplus-plan", "byteplus"},
		{"openai", "openai"},
		{"bedrock", "amazon-bedrock"},
	}

	for _, tt := range tests {
		result := NormalizeProviderIDForAuth(tt.input)
		if result != tt.expected {
			t.Errorf("NormalizeProviderIDForAuth(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// testAliasPlugin implements Plugin and AliasProvider for testing.
type testAliasPlugin struct {
	id      string
	label   string
	aliases []string
}

func (p *testAliasPlugin) ID() string                { return p.id }
func (p *testAliasPlugin) Label() string             { return p.label }
func (p *testAliasPlugin) AuthMethods() []AuthMethod { return nil }
func (p *testAliasPlugin) Aliases() []string         { return p.aliases }

func TestRegistry_GetByNormalizedID(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(&testAliasPlugin{
		id:      "amazon-bedrock",
		label:   "Amazon Bedrock",
		aliases: []string{"bedrock", "aws-bedrock"},
	})
	_ = r.Register(&testAliasPlugin{
		id:    "openai",
		label: "OpenAI",
	})

	// Direct match.
	p := r.ByNormalizedID("openai")
	if p == nil || p.ID() != "openai" {
		t.Error("expected openai via direct match")
	}

	// Normalized match.
	p = r.ByNormalizedID("bedrock")
	if p == nil || p.ID() != "amazon-bedrock" {
		t.Error("expected amazon-bedrock via normalization")
	}

	// Alias match.
	p = r.ByNormalizedID("aws-bedrock")
	if p == nil || p.ID() != "amazon-bedrock" {
		t.Error("expected amazon-bedrock via alias")
	}

	// Not found.
	p = r.ByNormalizedID("nonexistent")
	if p != nil {
		t.Error("expected nil for unknown provider")
	}
}
