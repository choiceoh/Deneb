package cmddispatch

import "testing"

func TestParseSlashCommandOrNull(t *testing.T) {
	parsed := ParseSlashCommandOrNull("/config set model gpt-5", "/config", "invalid", "show")
	if parsed == nil || !parsed.OK {
		t.Fatalf("expected parsed command, got %#v", parsed)
	}
	if parsed.Action != "set" || parsed.Args != "model gpt-5" {
		t.Fatalf("unexpected parse result: %#v", parsed)
	}
}

func TestParseSlashCommandOrNullDefaultAction(t *testing.T) {
	parsed := ParseSlashCommandOrNull("/config", "/config", "invalid", "")
	if parsed == nil || !parsed.OK {
		t.Fatalf("expected parsed command, got %#v", parsed)
	}
	if parsed.Action != "show" {
		t.Fatalf("expected default action 'show', got %q", parsed.Action)
	}
}
