package plugin

import "testing"

func TestResolveModelID_WithAlias(t *testing.T) {
	cfg := &ModelProviderConfig{
		ModelAliases: map[string]string{
			"fast":  "claude-3-haiku-20240307",
			"smart": "claude-sonnet-4-20250514",
		},
	}
	got := cfg.ResolveModelID("fast")
	if got != "claude-3-haiku-20240307" {
		t.Errorf("expected claude-3-haiku-20240307, got %q", got)
	}
	got = cfg.ResolveModelID("smart")
	if got != "claude-sonnet-4-20250514" {
		t.Errorf("expected claude-sonnet-4-20250514, got %q", got)
	}
}

func TestResolveModelID_NoAlias(t *testing.T) {
	cfg := &ModelProviderConfig{
		ModelAliases: map[string]string{"fast": "claude-3-haiku"},
	}
	got := cfg.ResolveModelID("claude-sonnet-4-20250514")
	if got != "claude-sonnet-4-20250514" {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestResolveModelID_NilMap(t *testing.T) {
	cfg := &ModelProviderConfig{}
	got := cfg.ResolveModelID("any-model")
	if got != "any-model" {
		t.Errorf("expected passthrough, got %q", got)
	}
}
