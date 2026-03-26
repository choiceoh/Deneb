package autoreply

import (
	"strings"
	"testing"
)

func TestParseInlineDirectives(t *testing.T) {
	t.Run("no directives", func(t *testing.T) {
		d := ParseInlineDirectives("hello world", nil)
		if d.HasThinkDirective || d.HasVerboseDirective || d.HasFastDirective {
			t.Error("expected no directives")
		}
		if d.Cleaned != "hello world" {
			t.Errorf("cleaned = %q, want 'hello world'", d.Cleaned)
		}
	})

	t.Run("think directive", func(t *testing.T) {
		d := ParseInlineDirectives("hello /think high world", nil)
		if !d.HasThinkDirective {
			t.Error("expected think directive")
		}
		if d.ThinkLevel != ThinkHigh {
			t.Errorf("ThinkLevel = %q, want 'high'", d.ThinkLevel)
		}
		if d.Cleaned != "hello world" {
			t.Errorf("cleaned = %q, want 'hello world'", d.Cleaned)
		}
	})

	t.Run("think without arg defaults to low", func(t *testing.T) {
		d := ParseInlineDirectives("/think", nil)
		if !d.HasThinkDirective {
			t.Error("expected think directive")
		}
		if d.ThinkLevel != ThinkLow {
			t.Errorf("ThinkLevel = %q, want 'low'", d.ThinkLevel)
		}
	})

	t.Run("verbose directive", func(t *testing.T) {
		d := ParseInlineDirectives("/verbose full", nil)
		if !d.HasVerboseDirective {
			t.Error("expected verbose directive")
		}
		if d.VerboseLevel != VerboseFull {
			t.Errorf("VerboseLevel = %q, want 'full'", d.VerboseLevel)
		}
	})

	t.Run("fast directive", func(t *testing.T) {
		d := ParseInlineDirectives("check this /fast", nil)
		if !d.HasFastDirective {
			t.Error("expected fast directive")
		}
		if !d.FastMode {
			t.Error("expected FastMode = true")
		}
	})

	t.Run("multiple directives", func(t *testing.T) {
		d := ParseInlineDirectives("hello /think medium /fast /verbose on", nil)
		if !d.HasThinkDirective || !d.HasFastDirective || !d.HasVerboseDirective {
			t.Error("expected all three directives")
		}
		if d.ThinkLevel != ThinkMedium {
			t.Errorf("ThinkLevel = %q", d.ThinkLevel)
		}
		if !strings.Contains(d.Cleaned, "hello") {
			t.Errorf("cleaned = %q, should contain 'hello'", d.Cleaned)
		}
	})

	t.Run("model directive", func(t *testing.T) {
		d := ParseInlineDirectives("hey /model gpt-4 what's up", nil)
		if !d.HasModelDirective {
			t.Error("expected model directive")
		}
		if d.RawModelDirective != "gpt-4" {
			t.Errorf("RawModelDirective = %q, want 'gpt-4'", d.RawModelDirective)
		}
	})

	t.Run("reasoning directive", func(t *testing.T) {
		d := ParseInlineDirectives("/reasoning stream", nil)
		if !d.HasReasoningDirective {
			t.Error("expected reasoning directive")
		}
		if d.ReasoningLevel != ReasoningStream {
			t.Errorf("ReasoningLevel = %q, want 'stream'", d.ReasoningLevel)
		}
	})

	t.Run("elevated disabled", func(t *testing.T) {
		d := ParseInlineDirectives("/elevated on hello", &DirectiveParseOptions{DisableElevated: true})
		if d.HasElevatedDirective {
			t.Error("elevated should be disabled")
		}
	})
}

func TestIsDirectiveOnly(t *testing.T) {
	t.Run("directive only", func(t *testing.T) {
		d := ParseInlineDirectives("/think high", nil)
		if !IsDirectiveOnly(d) {
			t.Error("expected directive-only")
		}
	})

	t.Run("directive with text", func(t *testing.T) {
		d := ParseInlineDirectives("/think high hello", nil)
		if IsDirectiveOnly(d) {
			t.Error("expected not directive-only")
		}
	})

	t.Run("no directives", func(t *testing.T) {
		d := ParseInlineDirectives("hello world", nil)
		if IsDirectiveOnly(d) {
			t.Error("expected not directive-only")
		}
	})
}
