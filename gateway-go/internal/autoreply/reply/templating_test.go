package reply

import (
	"strings"
	"testing"
	"time"
)

// --- ApplyTemplate ---

func TestApplyTemplate_NoPlaceholders(t *testing.T) {
	vars := TemplateVars{AgentID: "agent1"}
	got := ApplyTemplate("hello world", vars)
	if got != "hello world" {
		t.Errorf("no placeholders: expected unchanged, got %q", got)
	}
}

func TestApplyTemplate_Empty(t *testing.T) {
	got := ApplyTemplate("", TemplateVars{})
	if got != "" {
		t.Errorf("empty template should return empty, got %q", got)
	}
}

func TestApplyTemplate_AgentID(t *testing.T) {
	got := ApplyTemplate("agent={{agentId}}", TemplateVars{AgentID: "abc123"})
	if got != "agent=abc123" {
		t.Errorf("expected agent=abc123, got %q", got)
	}
}

func TestApplyTemplate_Model(t *testing.T) {
	got := ApplyTemplate("model={{model}}", TemplateVars{Model: "claude-3"})
	if got != "model=claude-3" {
		t.Errorf("expected model=claude-3, got %q", got)
	}
}

func TestApplyTemplate_Provider(t *testing.T) {
	got := ApplyTemplate("provider={{provider}}", TemplateVars{Provider: "anthropic"})
	if got != "provider=anthropic" {
		t.Errorf("expected provider=anthropic, got %q", got)
	}
}

func TestApplyTemplate_Channel(t *testing.T) {
	got := ApplyTemplate("ch={{channel}}", TemplateVars{Channel: "telegram"})
	if got != "ch=telegram" {
		t.Errorf("expected ch=telegram, got %q", got)
	}
}

func TestApplyTemplate_IsGroupTrue(t *testing.T) {
	got := ApplyTemplate("group={{isGroup}}", TemplateVars{IsGroup: true})
	if got != "group=true" {
		t.Errorf("expected group=true, got %q", got)
	}
}

func TestApplyTemplate_IsGroupFalse(t *testing.T) {
	got := ApplyTemplate("group={{isGroup}}", TemplateVars{IsGroup: false})
	if got != "group=false" {
		t.Errorf("expected group=false, got %q", got)
	}
}

func TestApplyTemplate_MultipleVars(t *testing.T) {
	got := ApplyTemplate("{{model}}/{{provider}}", TemplateVars{Model: "gpt-4", Provider: "openai"})
	if got != "gpt-4/openai" {
		t.Errorf("expected gpt-4/openai, got %q", got)
	}
}

func TestApplyTemplate_Timestamp(t *testing.T) {
	ts := time.Date(2026, 3, 29, 12, 0, 0, 0, time.UTC)
	got := ApplyTemplate("ts={{timestamp}}", TemplateVars{Timestamp: ts})
	// Should contain RFC3339 formatted timestamp
	if !strings.Contains(got, "2026-03-29") {
		t.Errorf("expected timestamp in output, got %q", got)
	}
}

func TestApplyTemplate_ZeroTimestampEmpty(t *testing.T) {
	got := ApplyTemplate("ts={{timestamp}}", TemplateVars{Timestamp: time.Time{}})
	// Zero time formats to empty string.
	if got != "ts=" {
		t.Errorf("zero timestamp should produce empty replacement, got %q", got)
	}
}

func TestApplyTemplate_UnknownPlaceholderPreserved(t *testing.T) {
	got := ApplyTemplate("{{unknown}}", TemplateVars{})
	if got != "{{unknown}}" {
		t.Errorf("unknown placeholder should be left as-is, got %q", got)
	}
}

func TestApplyTemplate_FromTo(t *testing.T) {
	got := ApplyTemplate("{{from}}->{{to}}", TemplateVars{From: "user", To: "bot"})
	if got != "user->bot" {
		t.Errorf("expected user->bot, got %q", got)
	}
}

// --- ResolveCurrentTimeString ---

func TestResolveCurrentTimeString_ReturnsNonEmpty(t *testing.T) {
	got := ResolveCurrentTimeString("utc")
	if got == "" {
		t.Error("expected non-empty time string")
	}
}

func TestResolveCurrentTimeString_ContainsDate(t *testing.T) {
	got := ResolveCurrentTimeString("")
	// Should be formatted as "2006-01-02 15:04:05 MST".
	// Just check it has a dash-separated date part.
	if len(got) < 10 || got[4] != '-' || got[7] != '-' {
		t.Errorf("unexpected time format: %q", got)
	}
}

func TestResolveCurrentTimeString_InvalidTimezoneDoesNotPanic(t *testing.T) {
	// Should not panic or error, falls back to local time.
	got := ResolveCurrentTimeString("Not/AReal_Zone")
	if got == "" {
		t.Error("expected fallback non-empty time string")
	}
}
