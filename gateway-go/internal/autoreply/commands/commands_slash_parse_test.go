package commands

import (
	"testing"
)

func TestParseSlashCommandActionArgs(t *testing.T) {
	tests := []struct {
		name       string
		raw, slash string
		wantKind   SlashParseKind
		wantAction string
		wantArgs   string
	}{
		{"no match", "hello", "/debug", SlashNoMatch, "", ""},
		{"empty", "/debug", "/debug", SlashEmpty, "", ""},
		{"empty with space", "/debug  ", "/debug", SlashEmpty, "", ""},
		{"show", "/debug show", "/debug", SlashParsed, "show", ""},
		{"set with args", "/debug set foo=bar", "/debug", SlashParsed, "set", "foo=bar"},
		{"case insensitive", "/DEBUG show", "/debug", SlashParsed, "show", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseSlashCommandActionArgs(tt.raw, tt.slash)
			if result.Kind != tt.wantKind {
				t.Errorf("kind = %d, want %d", result.Kind, tt.wantKind)
			}
			if result.Action != tt.wantAction {
				t.Errorf("action = %q, want %q", result.Action, tt.wantAction)
			}
			if result.Args != tt.wantArgs {
				t.Errorf("args = %q, want %q", result.Args, tt.wantArgs)
			}
		})
	}
}

func TestParseSlashCommandOrNull(t *testing.T) {
	// No match returns nil.
	if r := ParseSlashCommandOrNull("hello", "/debug", "invalid", ""); r != nil {
		t.Error("expected nil for non-matching input")
	}

	// Empty defaults to "show".
	r := ParseSlashCommandOrNull("/debug", "/debug", "invalid", "")
	if r == nil || !r.OK || r.Action != "show" {
		t.Errorf("empty should default to show, got %+v", r)
	}

	// Custom default action.
	r = ParseSlashCommandOrNull("/mcp", "/mcp", "invalid", "list")
	if r == nil || !r.OK || r.Action != "list" {
		t.Errorf("empty with custom default, got %+v", r)
	}

	// Parsed action.
	r = ParseSlashCommandOrNull("/debug set x=1", "/debug", "invalid", "")
	if r == nil || !r.OK || r.Action != "set" || r.Args != "x=1" {
		t.Errorf("parsed action, got %+v", r)
	}
}
