package model

import (
	"strings"
	"testing"
)

func TestFormatProviderModelRef(t *testing.T) {
	if got := FormatProviderModelRef("anthropic", "claude-3"); got != "anthropic/claude-3" {
		t.Errorf("got %q", got)
	}
	if got := FormatProviderModelRef("", "claude-3"); got != "claude-3" {
		t.Errorf("got %q", got)
	}
}

func TestBuildFallbackNotice(t *testing.T) {
	t.Run("same model returns empty", func(t *testing.T) {
		got := BuildFallbackNotice("anthropic", "claude-3", "anthropic", "claude-3", nil)
		if got != "" {
			t.Errorf("expected empty, got %q", got)
		}
	})

	t.Run("different model returns notice", func(t *testing.T) {
		got := BuildFallbackNotice("anthropic", "claude-3", "openai", "gpt-4",
			[]FallbackAttempt{{Reason: "rate_limited"}})
		if got == "" {
			t.Error("expected non-empty notice")
		}
		if !strings.HasPrefix(got, "↪") {
			t.Errorf("expected arrow prefix, got %q", got)
		}
	})
}

func TestBuildFallbackClearedNotice(t *testing.T) {
	got := BuildFallbackClearedNotice("anthropic", "claude-3", "openai/gpt-4")
	if got == "" {
		t.Error("expected non-empty notice")
	}
}

func TestResolveFallbackTransition(t *testing.T) {
	t.Run("no fallback", func(t *testing.T) {
		result := ResolveFallbackTransition("anthropic", "claude-3", "anthropic", "claude-3", nil, nil)
		if result.FallbackActive {
			t.Error("expected no fallback")
		}
	})

	t.Run("fallback active", func(t *testing.T) {
		result := ResolveFallbackTransition("anthropic", "claude-3", "openai", "gpt-4",
			[]FallbackAttempt{{Reason: "rate_limited"}}, nil)
		if !result.FallbackActive {
			t.Error("expected fallback active")
		}
		if !result.FallbackTransitioned {
			t.Error("expected fallback transitioned")
		}
	})

	t.Run("fallback cleared", func(t *testing.T) {
		prev := &FallbackNoticeState{
			SelectedModel: "anthropic/claude-3",
			ActiveModel:   "openai/gpt-4",
		}
		result := ResolveFallbackTransition("anthropic", "claude-3", "anthropic", "claude-3", nil, prev)
		if result.FallbackActive {
			t.Error("expected no fallback")
		}
		if !result.FallbackCleared {
			t.Error("expected fallback cleared")
		}
	})
}
