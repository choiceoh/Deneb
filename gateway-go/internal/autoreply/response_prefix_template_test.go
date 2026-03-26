package autoreply

import "testing"

func TestResolveResponsePrefixTemplate(t *testing.T) {
	tests := []struct {
		template string
		ctx      ResponsePrefixContext
		want     string
	}{
		{
			"[{model} | think:{thinkingLevel}]",
			ResponsePrefixContext{Model: "gpt-5.2", ThinkingLevel: "high"},
			"[gpt-5.2 | think:high]",
		},
		{
			"{provider}/{model}",
			ResponsePrefixContext{Provider: "openai", Model: "gpt-5.2"},
			"openai/gpt-5.2",
		},
		{
			"{unknown}",
			ResponsePrefixContext{},
			"{unknown}",
		},
		{
			"",
			ResponsePrefixContext{Model: "test"},
			"",
		},
		{
			"{Model} {MODEL}",
			ResponsePrefixContext{Model: "claude"},
			"claude claude",
		},
		{
			"[{identity.name}] {think}",
			ResponsePrefixContext{IdentityName: "Pi", ThinkingLevel: "low"},
			"[Pi] low",
		},
	}

	for _, tt := range tests {
		got := ResolveResponsePrefixTemplate(tt.template, tt.ctx)
		if got != tt.want {
			t.Errorf("ResolveResponsePrefixTemplate(%q, ...) = %q, want %q", tt.template, got, tt.want)
		}
	}
}

func TestExtractShortModelName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"openai-codex/gpt-5.2", "gpt-5.2"},
		{"claude-opus-4-6-20260205", "claude-opus-4-6"},
		{"gpt-5.2-latest", "gpt-5.2"},
		{"gpt-5.2", "gpt-5.2"},
		{"anthropic/claude-opus-4-6-20260101", "claude-opus-4-6"},
	}

	for _, tt := range tests {
		got := ExtractShortModelName(tt.input)
		if got != tt.want {
			t.Errorf("ExtractShortModelName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestHasTemplateVariables(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"{model}", true},
		{"no variables", false},
		{"{}", false},
		{"", false},
		{"{a1.b2}", true},
	}

	for _, tt := range tests {
		got := HasTemplateVariables(tt.input)
		if got != tt.want {
			t.Errorf("HasTemplateVariables(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}
